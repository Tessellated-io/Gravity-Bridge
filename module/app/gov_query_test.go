package app

import (
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	"github.com/cosmos/gogoproto/proto"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"

	gravitytypes "github.com/Gravity-Bridge/Gravity-Bridge/module/x/gravity/types"
)

const (
	legacyProposalTypeURL = "/gravity.v1.IBCMetadataProposal"
	legacyProposalID      = uint64(2)
	legacyIBCDenom        = "ibc/ABCD"
	legacyDecodeError     = "no concrete type registered for type URL " + legacyProposalTypeURL
	govQueryTestChainID   = "gravity-test"
)

// legacyIBCMetadataProposal builds the deprecated content type the way it still occurs in historical
// gravity-bridge-3 proposals (7, 8, 9, ...).
func legacyIBCMetadataProposal() *gravitytypes.IBCMetadataProposal {
	// nolint: exhaustruct
	return &gravitytypes.IBCMetadataProposal{
		Title:       "ibc metadata",
		Description: "deprecated proposal type which is still present in historical state",
		Metadata: banktypes.Metadata{
			Description: "test ibc token",
			DenomUnits:  []*banktypes.DenomUnit{{Denom: legacyIBCDenom, Exponent: 0, Aliases: nil}},
			Base:        legacyIBCDenom,
			Display:     legacyIBCDenom,
			Name:        "ABCD",
			Symbol:      "ABCD",
		},
		IbcDenom: legacyIBCDenom,
	}
}

// seedGovProposals writes two passed proposals straight into the gov store: proposal 1 wraps an ordinary
// TextProposal, proposal 2 wraps the deprecated IBCMetadataProposal exactly like the historical proposals do
// (MsgExecLegacyContent around the legacy content). Encoding never needs to resolve the content type, so this works
// through the consensus keeper.
func seedGovProposals(t *testing.T, app *Gravity, ctx sdk.Context) {
	t.Helper()
	authority := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	textContent, err := codectypes.NewAnyWithValue(&govv1beta1.TextProposal{Title: "text", Description: "text proposal"})
	require.NoError(t, err)
	textMsg, err := codectypes.NewAnyWithValue(&govv1.MsgExecLegacyContent{Content: textContent, Authority: authority})
	require.NoError(t, err)

	legacyContent, err := codectypes.NewAnyWithValue(legacyIBCMetadataProposal())
	require.NoError(t, err)
	require.Equal(t, legacyProposalTypeURL, legacyContent.TypeUrl)
	legacyMsg, err := codectypes.NewAnyWithValue(&govv1.MsgExecLegacyContent{Content: legacyContent, Authority: authority})
	require.NoError(t, err)

	tally := govv1.EmptyTallyResult()
	now := ctx.BlockTime()
	mk := func(id uint64, msg *codectypes.Any, title string) govv1.Proposal {
		// nolint: exhaustruct
		return govv1.Proposal{
			Id:               id,
			Messages:         []*codectypes.Any{msg},
			Status:           govv1.StatusPassed,
			FinalTallyResult: &tally,
			SubmitTime:       &now,
			DepositEndTime:   &now,
			VotingStartTime:  &now,
			VotingEndTime:    &now,
			Title:            title,
			Summary:          title,
		}
	}
	require.NoError(t, app.GovKeeper.Proposals.Set(ctx, 1, mk(1, textMsg, "text")))
	require.NoError(t, app.GovKeeper.Proposals.Set(ctx, legacyProposalID, mk(legacyProposalID, legacyMsg, "ibc metadata")))
}

// govQueryTestContext returns a finalize-block context for the test app.
func govQueryTestContext(app *Gravity) sdk.Context {
	// nolint: exhaustruct
	return app.BaseApp.NewContextLegacy(false, cmtproto.Header{Height: 1, ChainID: govQueryTestChainID})
}

// abciQuery runs a gRPC query through the app's query router exactly like an ABCI query (and therefore REST/gRPC)
// would, and returns the raw response bytes.
func abciQuery(t *testing.T, app *Gravity, ctx sdk.Context, path string, req proto.Message) []byte {
	t.Helper()
	route := app.GRPCQueryRouter().Route(path)
	require.NotNil(t, route, "no query route for %s", path)
	reqBz, err := app.AppCodec.Marshal(req)
	require.NoError(t, err)
	res, err := route(ctx, &abci.RequestQuery{Data: reqBz, Path: path, Height: 0, Prove: false})
	require.NoError(t, err, "query %s", path)
	return res.Value
}

func TestGovQueriesDecodeDeprecatedProposalContent(t *testing.T) {
	app := InitGravityTestApp(true)
	ctx := govQueryTestContext(app)
	seedGovProposals(t, app, ctx)

	// The consensus keeper still cannot decode the legacy proposal: consensus behaviour is unchanged.
	_, err := app.GovKeeper.Proposals.Get(ctx, legacyProposalID)
	require.ErrorContains(t, err, legacyDecodeError)

	// The query keeper can.
	proposal, err := app.GovQueryKeeper.Proposals.Get(ctx, legacyProposalID)
	require.NoError(t, err)
	legacyMsg, ok := proposal.Messages[0].GetCachedValue().(*govv1.MsgExecLegacyContent)
	require.True(t, ok)
	content, ok := legacyMsg.Content.GetCachedValue().(*gravitytypes.IBCMetadataProposal)
	require.True(t, ok)
	require.Equal(t, legacyIBCMetadataProposal().Title, content.Title)
	require.Equal(t, legacyIBCMetadataProposal().Metadata, content.Metadata)

	// The registered gov query services are served by the query keeper: the list queries which used to panic and the
	// single proposal queries which used to fail all succeed, on both the v1 and the v1beta1 API.
	// nolint: exhaustruct
	v1List := abciQuery(t, app, ctx, "/cosmos.gov.v1.Query/Proposals", &govv1.QueryProposalsRequest{})
	var v1ListResp govv1.QueryProposalsResponse
	require.NoError(t, app.ClientEncodingConfig.Codec.Unmarshal(v1List, &v1ListResp))
	require.Len(t, v1ListResp.Proposals, 2)
	require.Equal(t, legacyProposalID, v1ListResp.Proposals[1].Id)

	// nolint: exhaustruct
	v1beta1List := abciQuery(t, app, ctx, "/cosmos.gov.v1beta1.Query/Proposals", &govv1beta1.QueryProposalsRequest{})
	var v1beta1ListResp govv1beta1.QueryProposalsResponse
	require.NoError(t, app.ClientEncodingConfig.Codec.Unmarshal(v1beta1List, &v1beta1ListResp))
	require.Len(t, v1beta1ListResp.Proposals, 2)
	require.Equal(t, legacyProposalTypeURL, v1beta1ListResp.Proposals[1].Content.TypeUrl)

	v1Single := abciQuery(t, app, ctx, "/cosmos.gov.v1.Query/Proposal", &govv1.QueryProposalRequest{ProposalId: legacyProposalID})
	var v1SingleResp govv1.QueryProposalResponse
	require.NoError(t, app.ClientEncodingConfig.Codec.Unmarshal(v1Single, &v1SingleResp))
	require.Equal(t, legacyProposalID, v1SingleResp.Proposal.Id)

	tallyBz := abciQuery(t, app, ctx, "/cosmos.gov.v1beta1.Query/TallyResult", &govv1beta1.QueryTallyResultRequest{ProposalId: legacyProposalID})
	var tallyResp govv1beta1.QueryTallyResultResponse
	require.NoError(t, app.ClientEncodingConfig.Codec.Unmarshal(tallyBz, &tallyResp))
	require.True(t, tallyResp.Tally.Yes.IsZero())

	// Clients need the client encoding config to render these responses: the consensus codec cannot resolve the
	// deprecated content into JSON (which is what the REST gateway and the CLI do), the client codec can.
	var consensusView govv1beta1.QueryProposalsResponse
	require.NoError(t, app.AppCodec.Unmarshal(v1beta1List, &consensusView))
	_, err = app.AppCodec.MarshalJSON(&consensusView)
	require.ErrorContains(t, err, legacyProposalTypeURL)
	var clientView govv1beta1.QueryProposalsResponse
	require.NoError(t, app.ClientEncodingConfig.Codec.Unmarshal(v1beta1List, &clientView))
	jsonBz, err := app.ClientEncodingConfig.Codec.MarshalJSON(&clientView)
	require.NoError(t, err)
	require.Contains(t, string(jsonBz), `"@type":"`+legacyProposalTypeURL+`"`)
}

func TestGovExportGenesisWithDeprecatedProposalContent(t *testing.T) {
	app := InitGravityTestApp(true)
	ctx := govQueryTestContext(app)
	seedGovProposals(t, app, ctx)

	exported, err := app.ModuleManager.ExportGenesisForModules(ctx, app.AppCodec, []string{govtypes.ModuleName})
	require.NoError(t, err)
	var govGenesis govv1.GenesisState
	require.NoError(t, app.ClientEncodingConfig.Codec.UnmarshalJSON(exported[govtypes.ModuleName], &govGenesis))
	require.Len(t, govGenesis.Proposals, 2)
	require.Equal(t, legacyProposalID, govGenesis.Proposals[1].Id)
	require.Contains(t, string(exported[govtypes.ModuleName]), `"@type":"`+legacyProposalTypeURL+`"`)
}

// TestClientEncodingConfigResolvesDeprecatedContent guards the wiring of the client encoding config, which the CLI and
// the REST gateway obtain through NewEncodingConfig: it must resolve the deprecated content type, the consensus
// registry must not.
func TestClientEncodingConfigResolvesDeprecatedContent(t *testing.T) {
	app := InitGravityTestApp(false)
	_, err := app.InterfaceRegistry.Resolve(legacyProposalTypeURL)
	require.Error(t, err)
	_, err = app.ClientEncodingConfig.InterfaceRegistry.Resolve(legacyProposalTypeURL)
	require.NoError(t, err)
	_, err = NewEncodingConfig().InterfaceRegistry.Resolve(legacyProposalTypeURL)
	require.NoError(t, err)
}

// TestDeprecatedProposalContentStaysOutOfConsensus pins down the property the query registry split relies on: the
// deprecated type is invisible to everything that determines transaction results, so a node with this fix produces
// exactly the same results as a node without it.
func TestDeprecatedProposalContentStaysOutOfConsensus(t *testing.T) {
	app := InitGravityTestApp(true)
	ctx := govQueryTestContext(app)
	seedGovProposals(t, app, ctx)

	// A transaction carrying the deprecated content is rejected by the app's tx decoder (as it is by v1.14.1) even
	// though the client encoding config is able to encode and decode it.
	msg, err := govv1beta1.NewMsgSubmitProposal(legacyIBCMetadataProposal(), sdk.NewCoins(), AccAddresses[0])
	require.NoError(t, err)
	txBuilder := app.ClientEncodingConfig.TxConfig.NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(msg))
	txBytes, err := app.ClientEncodingConfig.TxConfig.TxEncoder()(txBuilder.GetTx())
	require.NoError(t, err)

	_, err = app.ClientEncodingConfig.TxConfig.TxDecoder()(txBytes)
	require.NoError(t, err)
	_, err = app.TxDecode(txBytes)
	require.ErrorIs(t, err, sdkerrors.ErrTxDecode)
	require.ErrorContains(t, err, legacyProposalTypeURL)

	// Message execution against a proposal which carries the deprecated content fails exactly as before the fix,
	// with the decode error rather than a gov error code.
	msgServer := govkeeper.NewMsgServerImpl(app.GovKeeper)
	_, err = msgServer.Deposit(ctx, &govv1.MsgDeposit{
		ProposalId: legacyProposalID,
		Depositor:  AccAddresses[0].String(),
		Amount:     sdk.NewCoins(sdk.NewInt64Coin("ugraviton", 1)),
	})
	require.ErrorContains(t, err, legacyDecodeError)

	// The deprecated type cannot be submitted as a new proposal either.
	require.Error(t, legacyIBCMetadataProposal().ValidateBasic())
	require.False(t, govv1beta1.IsValidProposalType(gravitytypes.ProposalTypeIBCMetadata))
}
