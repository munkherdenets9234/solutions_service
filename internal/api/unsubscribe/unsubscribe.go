// Package unsubscribe serves the public one-click unsubscribe route linked from
// every request email.
//
// It is mounted on /api/v1/public, outside the tenant API-key gate: a mail
// recipient's browser sends no X-API-Key and has no session. The tenant and
// the user come ONLY from the signed token. GET shows a confirmation form and
// changes nothing (mail scanners prefetch links); POST performs the change.
package unsubscribe

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"strings"
	"time"

	unsubtoken "github.com/eandstravel/digitalservice/internal/unsubscribe"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// UserOptOut flips a tenant user's request-email opt-in.
type UserOptOut interface {
	SetReceiveEmails(ctx context.Context, tenantID, id primitive.ObjectID, v bool) error
}

// OutboxCanceller drops a user's queued, unsent mail.
type OutboxCanceller interface {
	CancelPendingForUser(ctx context.Context, tenantID, userID primitive.ObjectID) (int64, error)
}

// Deps is what Register needs. With an empty Key or a nil repo the routes are
// not registered (mail is off, so there are no links to serve).
type Deps struct {
	Key       []byte
	Users     UserOptOut
	Outbox    OutboxCanceller
	RateLimit gin.HandlerFunc
	Now       func() time.Time
	Log       *zap.Logger
}

const maxBody = 4 << 10

// Fixed bodies. The invalid ones are constants so every invalid token yields
// the same bytes.
const (
	invalidHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Unsubscribe</title></head><body><h1>This link is not valid</h1><p>The unsubscribe link is invalid or has expired.</p></body></html>`
	invalidJSON = `{"success":false,"message":"This link is not valid"}`
	errorHTML   = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Unsubscribe</title></head><body><h1>Something went wrong</h1><p>Please try again later.</p></body></html>`
	errorJSON   = `{"success":false,"message":"Something went wrong"}`
	doneHTML    = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Unsubscribed</title></head><body><h1>You are unsubscribed</h1><p>You will no longer receive request emails.</p></body></html>`
	doneJSON    = `{"success":true,"message":"Unsubscribed"}`
)

type controller struct {
	key    []byte
	users  UserOptOut
	outbox OutboxCanceller
	now    func() time.Time
	log    *zap.Logger
}

// Register mounts GET and POST /unsubscribe on g.
func Register(g *gin.RouterGroup, d Deps) {
	if len(d.Key) == 0 || d.Users == nil || d.Outbox == nil {
		return
	}
	c := &controller{key: d.Key, users: d.Users, outbox: d.Outbox, now: d.Now, log: d.Log}
	if c.now == nil {
		c.now = time.Now
	}
	if c.log == nil {
		c.log = zap.NewNop()
	}
	chain := []gin.HandlerFunc{}
	if d.RateLimit != nil {
		chain = append(chain, d.RateLimit)
	}
	g.GET("/unsubscribe", append(chain, c.page)...)
	g.POST("/unsubscribe", append(chain, c.submit)...)
}

func harden(ctx *gin.Context) {
	h := ctx.Writer.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
}

func wantsJSON(ctx *gin.Context) bool {
	return strings.HasPrefix(ctx.GetHeader("Content-Type"), "application/json")
}

func respond(ctx *gin.Context, status int, htmlBody, jsonBody string) {
	harden(ctx)
	if wantsJSON(ctx) {
		ctx.Data(status, "application/json; charset=utf-8", []byte(jsonBody))
		return
	}
	ctx.Data(status, "text/html; charset=utf-8", []byte(htmlBody))
}

// page is the GET confirmation form. It does not look at the token's validity
// and carries no tenant data, so it reveals nothing and changes nothing.
func (c *controller) page(ctx *gin.Context) {
	tok := html.EscapeString(ctx.Query("token"))
	body := `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex"><title>Unsubscribe</title></head><body><h1>Unsubscribe from request emails</h1><p>Confirm that you no longer want to receive request emails.</p><form method="post" action="/api/v1/public/unsubscribe"><input type="hidden" name="token" value="` + tok + `"><button type="submit">Unsubscribe</button></form></body></html>`
	harden(ctx)
	ctx.Data(http.StatusOK, "text/html; charset=utf-8", []byte(body))
}

func readToken(ctx *gin.Context) string {
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, maxBody)
	if wantsJSON(ctx) {
		var in struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(ctx.Request.Body).Decode(&in); err != nil {
			return ""
		}
		return in.Token
	}
	return ctx.PostForm("token")
}

func (c *controller) submit(ctx *gin.Context) {
	invalid := func() { respond(ctx, http.StatusBadRequest, invalidHTML, invalidJSON) }

	tenantID, userID, err := unsubtoken.Verify(c.key, readToken(ctx), c.now())
	if err != nil {
		c.log.Info("unsubscribe", zap.String("outcome", "invalid_token"))
		invalid()
		return
	}

	if err := c.users.SetReceiveEmails(ctx.Request.Context(), tenantID, userID, false); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			c.log.Info("unsubscribe", zap.String("outcome", "unknown_user"))
			invalid()
			return
		}
		c.log.Error("unsubscribe", zap.String("outcome", "optout_failed"),
			zap.String("tenant_id", tenantID.Hex()), zap.String("user_id", userID.Hex()))
		respond(ctx, http.StatusInternalServerError, errorHTML, errorJSON)
		return
	}
	if _, err := c.outbox.CancelPendingForUser(ctx.Request.Context(), tenantID, userID); err != nil {
		// The opt-out already holds; the worker re-checks the flag before
		// sending, so a leftover row is not mailed.
		c.log.Error("unsubscribe", zap.String("outcome", "cancel_failed"),
			zap.String("tenant_id", tenantID.Hex()), zap.String("user_id", userID.Hex()))
		respond(ctx, http.StatusInternalServerError, errorHTML, errorJSON)
		return
	}
	c.log.Info("unsubscribe", zap.String("outcome", "ok"),
		zap.String("tenant_id", tenantID.Hex()), zap.String("user_id", userID.Hex()))
	respond(ctx, http.StatusOK, doneHTML, doneJSON)
}
