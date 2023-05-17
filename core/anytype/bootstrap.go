package anytype

import (
	"context"
	"os"

	"github.com/anytypeio/go-anytype-middleware/app"
	"github.com/anytypeio/go-anytype-middleware/core/account"
	"github.com/anytypeio/go-anytype-middleware/core/anytype/config"
	"github.com/anytypeio/go-anytype-middleware/core/block"
	"github.com/anytypeio/go-anytype-middleware/core/block/bookmark"
	decorator "github.com/anytypeio/go-anytype-middleware/core/block/bookmark/bookmarkimporter"
	"github.com/anytypeio/go-anytype-middleware/core/block/doc"
	"github.com/anytypeio/go-anytype-middleware/core/block/editor"
	"github.com/anytypeio/go-anytype-middleware/core/block/export"
	importer "github.com/anytypeio/go-anytype-middleware/core/block/import"
	"github.com/anytypeio/go-anytype-middleware/core/block/object"
	"github.com/anytypeio/go-anytype-middleware/core/block/process"
	"github.com/anytypeio/go-anytype-middleware/core/block/restriction"
	"github.com/anytypeio/go-anytype-middleware/core/block/source"
	"github.com/anytypeio/go-anytype-middleware/core/configfetcher"
	"github.com/anytypeio/go-anytype-middleware/core/debug"
	"github.com/anytypeio/go-anytype-middleware/core/event"
	"github.com/anytypeio/go-anytype-middleware/core/history"
	"github.com/anytypeio/go-anytype-middleware/core/indexer"
	"github.com/anytypeio/go-anytype-middleware/core/kanban"
	"github.com/anytypeio/go-anytype-middleware/core/recordsbatcher"
	"github.com/anytypeio/go-anytype-middleware/core/relation"
	"github.com/anytypeio/go-anytype-middleware/core/session"
	"github.com/anytypeio/go-anytype-middleware/core/status"
	"github.com/anytypeio/go-anytype-middleware/core/subscription"
	"github.com/anytypeio/go-anytype-middleware/core/wallet"
	"github.com/anytypeio/go-anytype-middleware/metrics"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/cafe"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/core"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/datastore/clientds"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/files"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/gateway"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/ipfs/ipfslite"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/localstore/filestore"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/localstore/ftsearch"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/localstore/objectstore"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/pin"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/profilefinder"
	"github.com/anytypeio/go-anytype-middleware/pkg/lib/threads"
	walletUtil "github.com/anytypeio/go-anytype-middleware/pkg/lib/wallet"
	"github.com/anytypeio/go-anytype-middleware/util/builtinobjects"
	"github.com/anytypeio/go-anytype-middleware/util/builtintemplate"
	"github.com/anytypeio/go-anytype-middleware/util/linkpreview"
	"github.com/anytypeio/go-anytype-middleware/util/unsplash"
)

func StartAccountRecoverApp(ctx context.Context, eventSender event.Sender, accountPrivKey walletUtil.Keypair) (a *app.App, err error) {
	a = new(app.App)
	device, err := walletUtil.NewRandomKeypair(walletUtil.KeypairTypeDevice)
	if err != nil {
		return nil, err
	}

	a.Register(wallet.NewWithRepoPathAndKeys("", accountPrivKey, device)).
		Register(config.New(
			config.WithStagingCafe(os.Getenv("ANYTYPE_STAGING") == "1"),
			config.DisableFileConfig(true), // do not load/save config to file because we don't have a libp2p node and repo in this mode
		),
		).
		Register(cafe.New()).
		Register(profilefinder.New()).
		Register(eventSender)

	if err = a.Start(ctx); err != nil {
		return
	}

	return a, nil
}

func BootstrapConfig(newAccount bool, isStaging bool) *config.Config {
	return config.New(
		config.WithStagingCafe(isStaging),
		config.WithNewAccount(newAccount),
	)
}

func BootstrapWallet(rootPath, accountId string) wallet.Wallet {
	return wallet.NewWithAccountRepo(rootPath, accountId)
}

func StartNewApp(ctx context.Context, components ...app.Component) (a *app.App, err error) {
	a = new(app.App)
	Bootstrap(a, components...)
	metrics.SharedClient.SetAppVersion(a.Version())
	metrics.SharedClient.Run()
	if err = a.Start(ctx); err != nil {
		metrics.SharedClient.Close()
		a = nil
		return
	}

	return
}

func Bootstrap(a *app.App, components ...app.Component) {
	for _, c := range components {
		a.Register(c)
	}
	a.Register(clientds.New()).
		Register(ftsearch.New()).
		Register(objectstore.New()).
		Register(relation.New()).
		Register(filestore.New()).
		Register(recordsbatcher.New()).
		Register(ipfslite.New()).
		Register(files.New()).
		Register(cafe.New()).
		Register(account.New()).
		Register(configfetcher.New()).
		Register(process.New()).
		Register(threads.New()).
		Register(source.New()).
		Register(core.New()).
		Register(builtintemplate.New()).
		Register(pin.New()).
		Register(status.New()).
		Register(block.New()).
		Register(indexer.New()).
		Register(history.New()).
		Register(gateway.New()).
		Register(export.New()).
		Register(linkpreview.New()).
		Register(unsplash.New()).
		Register(restriction.New()).
		Register(debug.New()).
		Register(doc.New()).
		Register(subscription.New()).
		Register(builtinobjects.New()).
		Register(bookmark.New()).
		Register(session.New()).
		Register(importer.New()).
		Register(decorator.New()).
		Register(object.NewCreator()).
		Register(kanban.New()).
		Register(editor.NewObjectFactory())
	return
}
