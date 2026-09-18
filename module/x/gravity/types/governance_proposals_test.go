package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
)

const (
	ibcMetadataProposalTypeURL = "/gravity.v1.IBCMetadataProposal"
	ibcMetadataTestDenom       = "ibc/ABCD"
	ibcMetadataTestTitle       = "title"
	ibcMetadataTestDescription = "description"
)

func TestIBCMetadataProposalIsDeprecatedContent(t *testing.T) {
	// nolint: exhaustruct
	var content govv1beta1.Content = &IBCMetadataProposal{
		Title:       ibcMetadataTestTitle,
		Description: ibcMetadataTestDescription,
		Metadata:    banktypes.Metadata{Base: ibcMetadataTestDenom, Display: ibcMetadataTestDenom, Name: "ABCD", Symbol: "ABCD"},
		IbcDenom:    ibcMetadataTestDenom,
	}
	require.Equal(t, ibcMetadataTestTitle, content.GetTitle())
	require.Equal(t, ibcMetadataTestDescription, content.GetDescription())
	require.Equal(t, RouterKey, content.ProposalRoute())
	require.Equal(t, ProposalTypeIBCMetadata, content.ProposalType())
	// New proposals of this type must be rejected
	require.Error(t, content.ValidateBasic())
	require.False(t, govv1beta1.IsValidProposalType(ProposalTypeIBCMetadata))
	require.NotEmpty(t, content.String())
}

// TestIBCMetadataProposalRegistrationSplit checks that the deprecated content type is only resolvable after
// RegisterLegacyQueryInterfaces, i.e. that the regular (consensus) RegisterInterfaces does not register it.
func TestIBCMetadataProposalRegistrationSplit(t *testing.T) {
	registry := codectypes.NewInterfaceRegistry()
	govv1beta1.RegisterInterfaces(registry)
	govv1.RegisterInterfaces(registry)
	RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)

	// nolint: exhaustruct
	content, err := codectypes.NewAnyWithValue(&IBCMetadataProposal{Title: ibcMetadataTestTitle, Description: ibcMetadataTestDescription, IbcDenom: ibcMetadataTestDenom})
	require.NoError(t, err)
	require.Equal(t, ibcMetadataProposalTypeURL, content.TypeUrl)
	bz, err := cdc.Marshal(&govv1.MsgExecLegacyContent{Content: content, Authority: "gravity1authority"})
	require.NoError(t, err)

	_, err = registry.Resolve(ibcMetadataProposalTypeURL)
	require.Error(t, err)
	// nolint: exhaustruct
	require.ErrorContains(t, cdc.Unmarshal(bz, &govv1.MsgExecLegacyContent{}), "no concrete type registered for type URL "+ibcMetadataProposalTypeURL)

	RegisterLegacyQueryInterfaces(registry)

	_, err = registry.Resolve(ibcMetadataProposalTypeURL)
	require.NoError(t, err)
	// nolint: exhaustruct
	var decoded govv1.MsgExecLegacyContent
	require.NoError(t, cdc.Unmarshal(bz, &decoded))
	proposal, ok := decoded.Content.GetCachedValue().(*IBCMetadataProposal)
	require.True(t, ok)
	require.Equal(t, ibcMetadataTestDenom, proposal.IbcDenom)
}
