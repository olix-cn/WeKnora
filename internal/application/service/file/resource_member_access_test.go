package file

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type restrictedCatalogStub struct {
	catalogStub
	denied    error
	owner     uint64
	anonymous bool
}

func (s *restrictedCatalogStub) AuthorizeResourceMemberAccess(
	_ context.Context, tenantID uint64, _ string, anonymous bool,
) error {
	s.owner, s.anonymous = tenantID, anonymous
	return s.denied
}

func TestRestrictedResourceCannotFallBackToCloudProviderURL(t *testing.T) {
	t.Setenv("APP_EXTERNAL_URL", "")
	denied := errors.New("synthetic member-policy denial")
	catalog := &restrictedCatalogStub{denied: denied}
	catalog.ref = "resource://AbCdEfGhIjKlMnOpQrStUv"
	catalog.resource = &types.StoredResource{TenantID: 7, PhysicalPath: "s3://bucket/private.pdf"}
	svc := NewResourceCatalogFileService(&physicalFileStub{}, catalog)
	url, err := svc.GetFileURL(context.Background(), catalog.ref)
	require.ErrorIs(t, err, denied)
	require.Empty(t, url)
	require.EqualValues(t, 7, catalog.owner)
	require.True(t, catalog.anonymous)
	ctx := types.WithExecutionTenant(context.Background(), 7)
	url, err = svc.GetFileURL(ctx, "s3://bucket/private.pdf")
	require.ErrorIs(t, err, denied)
	require.Empty(t, url)
}

func TestRestrictedResourceBlocksEveryProviderBeforeNativeSigning(t *testing.T) {
	for _, provider := range []string{"s3", "minio", "cos", "oss", "obs", "tos", "ks3", "local"} {
		t.Run(provider, func(t *testing.T) {
			t.Setenv("APP_EXTERNAL_URL", "")
			denied := errors.New("synthetic policy denial")
			catalog := &restrictedCatalogStub{denied: denied}
			catalog.ref = "resource://AbCdEfGhIjKlMnOpQrStUv"
			catalog.resource = &types.StoredResource{TenantID: 7, PhysicalPath: provider + "://private/file.pdf"}
			// The inner signer is nil. Any attempt to sign before checking
			// policy would panic, independent of the cloud SDK implementation.
			svc := &resourceCatalogFileService{catalog: catalog}
			url, err := svc.GetFileURL(context.Background(), catalog.ref)
			require.ErrorIs(t, err, denied)
			require.Empty(t, url)
		})
	}
}
