package app

import (
	"encoding/json"
	"fmt"

	gogogrpc "github.com/cosmos/gogoproto/grpc"
	"google.golang.org/grpc"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
)

// This file wires the x/gov module so that its gRPC/REST QUERY services and its genesis export are served by a
// second, read-only gov keeper whose codec can decode deprecated proposal content types, while every consensus
// code path keeps using the regular gov keeper and codec.
//
// Background: v1.14.0 removed the gravity.v1.IBCMetadataProposal type, but 40 historical gravity-bridge-3
// proposals (7, 8, 9, ...) still carry it in the x/gov store. With a single interface registry every query that
// lists proposals fails to decode them (and, through an SDK pagination bug, answers with a nil pointer panic), as
// does `gravity export`. Restoring the type in the consensus interface registry would instead change the result
// codes of failed transactions which touch those proposals (e.g. a MsgDeposit on proposal 7), which is
// consensus-breaking unless every validator switches binaries at the same height. Serving queries from a
// separate registry (see gravitytypes.RegisterLegacyQueryInterfaces and NewGravityApp) avoids that entirely:
//
//   - the tx decoder, message servers, migrations, genesis import, invariants and block hooks use the consensus
//     keeper/codec exactly as before,
//   - only the query services below and ExportGenesis use the query keeper/codec.

const (
	govV1QueryServiceName      = "cosmos.gov.v1.Query"
	govV1Beta1QueryServiceName = "cosmos.gov.v1beta1.Query"
)

// govAppModule wraps gov.AppModule, see the file comment.
type govAppModule struct {
	gov.AppModule
	queryKeeper *govkeeper.Keeper
	queryCodec  codec.Codec
}

// newGovAppModule wraps base so that its query services and genesis export are served by queryKeeper/queryCodec.
func newGovAppModule(base gov.AppModule, queryKeeper *govkeeper.Keeper, queryCodec codec.Codec) govAppModule {
	if queryKeeper == nil || queryCodec == nil {
		panic("Nil argument to newGovAppModule")
	}
	return govAppModule{
		AppModule:   base,
		queryKeeper: queryKeeper,
		queryCodec:  queryCodec,
	}
}

// RegisterServices registers the gov services exactly like gov.AppModule does (message servers and every store
// migration against the consensus keeper), except that the query service implementations are swapped for ones
// backed by the query keeper.
func (m govAppModule) RegisterServices(cfg module.Configurator) {
	m.AppModule.RegisterServices(govQueryConfigurator{Configurator: cfg, queryKeeper: m.queryKeeper})
}

// ExportGenesis exports the gov state through the query keeper and codec so that proposals which carry deprecated
// content types are exported instead of making `gravity export` panic. The consensus codec passed by the module
// manager is deliberately ignored: it cannot resolve those types.
func (m govAppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	gs, err := gov.ExportGenesis(ctx, m.queryKeeper)
	if err != nil {
		panic(err)
	}
	return m.queryCodec.MustMarshalJSON(gs)
}

// govQueryConfigurator is a module.Configurator whose QueryServer() substitutes the gov query service
// implementations. MsgServer() and RegisterMigration() pass straight through, so anything the SDK's gov module
// registers there (including any migration added by a future SDK bump) still targets the consensus keeper.
type govQueryConfigurator struct {
	module.Configurator
	queryKeeper *govkeeper.Keeper
}

func (c govQueryConfigurator) QueryServer() gogogrpc.Server {
	return govQueryServerRegistrar{Server: c.Configurator.QueryServer(), queryKeeper: c.queryKeeper}
}

// govQueryServerRegistrar replaces the handler of the two gov query services with query keeper backed ones and
// registers everything else untouched.
type govQueryServerRegistrar struct {
	gogogrpc.Server
	queryKeeper *govkeeper.Keeper
}

func (r govQueryServerRegistrar) RegisterService(sd *grpc.ServiceDesc, handler interface{}) {
	switch sd.ServiceName {
	case govV1QueryServiceName:
		if _, ok := handler.(govv1.QueryServer); !ok {
			panic(fmt.Sprintf("unexpected %s handler type %T, revisit gov_query.go", sd.ServiceName, handler))
		}
		handler = govkeeper.NewQueryServer(r.queryKeeper)
	case govV1Beta1QueryServiceName:
		if _, ok := handler.(govv1beta1.QueryServer); !ok {
			panic(fmt.Sprintf("unexpected %s handler type %T, revisit gov_query.go", sd.ServiceName, handler))
		}
		handler = govkeeper.NewLegacyQueryServer(r.queryKeeper)
	}
	r.Server.RegisterService(sd, handler)
}
