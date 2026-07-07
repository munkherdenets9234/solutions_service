package service

import (
	"context"
	"fmt"
	"io"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
	"github.com/eandstravel/digitalservice/pkg/apierr"
)

type UploadResult struct {
	URL      string `json:"url"`
	PublicID string `json:"public_id"`
}

type UploadService struct {
	cld *cloudinary.Cloudinary
}

func NewUploadService(cloudinaryURL string) (*UploadService, error) {
	cld, err := cloudinary.NewFromURL(cloudinaryURL)
	if err != nil {
		return nil, fmt.Errorf("cloudinary init: %w", err)
	}
	return &UploadService{cld: cld}, nil
}

// Upload streams file into the given Cloudinary folder and returns its
// public URL. folder is optional; pass "" to upload to Cloudinary's root.
func (s *UploadService) Upload(ctx context.Context, file io.Reader, folder string) (*UploadResult, error) {
	params := uploader.UploadParams{}
	if folder != "" {
		params.Folder = folder
	}

	res, err := s.cld.Upload.Upload(ctx, file, params)
	if err != nil {
		return nil, apierr.New(502, "upload to cloudinary failed")
	}
	return &UploadResult{URL: res.SecureURL, PublicID: res.PublicID}, nil
}
