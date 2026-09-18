package cmd

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	clitestutil "github.com/cosmos/cosmos-sdk/testutil/cli"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	"github.com/Gravity-Bridge/Gravity-Bridge/module/app"
	gravitytypes "github.com/Gravity-Bridge/Gravity-Bridge/module/x/gravity/types"
)

func TestValidateGenesisCmdRejectsQueryOnlyProposalTypes(t *testing.T) {
	tempApp := app.TemporaryApp()
	genesisState := tempApp.DefaultGenesis()

	legacyContent, err := codectypes.NewAnyWithValue(&gravitytypes.IBCMetadataProposal{
		Title:       "legacy IBC metadata",
		Description: "query-only proposal type",
		Metadata:    banktypes.Metadata{},
		IbcDenom:    "ibc/ABCD",
	})
	require.NoError(t, err)
	legacyMsg, err := codectypes.NewAnyWithValue(&govv1.MsgExecLegacyContent{
		Content:   legacyContent,
		Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
	})
	require.NoError(t, err)

	var govGenesis govv1.GenesisState
	require.NoError(t, tempApp.AppCodec.UnmarshalJSON(genesisState[govtypes.ModuleName], &govGenesis))
	// nolint: exhaustruct
	govGenesis.Proposals = append(govGenesis.Proposals, &govv1.Proposal{
		Id:       govGenesis.StartingProposalId,
		Messages: []*codectypes.Any{legacyMsg},
	})
	genesisState[govtypes.ModuleName] = tempApp.ClientEncodingConfig.Codec.MustMarshalJSON(&govGenesis)

	appState, err := json.Marshal(genesisState)
	require.NoError(t, err)
	genesisFile := filepath.Join(t.TempDir(), "genesis.json")
	require.NoError(t, genutiltypes.NewAppGenesisWithVersion("gravity-test", appState).SaveAs(genesisFile))

	clientCtx := client.Context{}.
		WithCodec(tempApp.ClientEncodingConfig.Codec).
		WithInterfaceRegistry(tempApp.ClientEncodingConfig.InterfaceRegistry).
		WithTxConfig(tempApp.ClientEncodingConfig.TxConfig).
		WithLegacyAmino(tempApp.ClientEncodingConfig.Amino)
	_, err = clitestutil.ExecTestCLICmd(
		clientCtx,
		newValidateGenesisCmd(*tempApp.ModuleBasicManager, tempApp.EncodingConfig),
		[]string{genesisFile},
	)
	require.ErrorContains(t, err, "/gravity.v1.IBCMetadataProposal")
}
