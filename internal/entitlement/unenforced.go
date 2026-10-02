package entitlement

import (
	"context"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Unenforced is the Provider a deployment gets when the platform link is not
// configured. Every tenant comes back StatusUnknown: no plan, no modules, no
// limits.
//
// What that means at the gates is the important part, and it is not "deny":
//
//   - SubscriptionMiddleware lets StatusUnknown through, because "this
//     tenant has no subscription record" has always meant "not held to any
//     subscription state" here — provisioning one is a separate deliberate
//     act by a superadmin, and a tenant created five minutes ago should not
//     have their storefront refuse traffic in the gap.
//   - HasModule returns true for an empty module list, so module gates are
//     not enforced either.
//
// So an unconfigured deployment behaves exactly as this service did for an
// unprovisioned tenant before subscriptions moved to tenantcore. That is the
// deliberate choice: the alternative — refusing every write when a URL is
// missing from the environment — converts one absent variable into a total
// outage for every tenant at once.
//
// It is not silent. config.Features reports the link as disabled, it is
// logged at startup, and /readyz keeps saying so for as long as the process
// runs. A production deployment running on this is misconfigured, and the
// point of /readyz is that you can tell.
type Unenforced struct{}

var _ Provider = Unenforced{}

func (Unenforced) For(_ context.Context, tenantID primitive.ObjectID) (Entitlement, error) {
	return Entitlement{TenantID: tenantID, Status: StatusUnknown}, nil
}
