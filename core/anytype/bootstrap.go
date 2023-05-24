package anytype

import (
	"context"
	"os"

	"github.com/anyproto/anytype-heart/app"
	"github.com/anyproto/anytype-heart/core/account"
	"github.com/anyproto/anytype-heart/core/anytype/config"
	"github.com/anyproto/anytype-heart/core/block"
	"github.com/anyproto/anytype-heart/core/block/bookmark"
	decorator "github.com/anyproto/anytype-heart/core/block/bookmark/bookmarkimporter"
	"github.com/anyproto/anytype-heart/core/block/doc"
	"github.com/anyproto/anytype-heart/core/block/editor"
	"github.com/anyproto/anytype-heart/core/block/export"
	importer "github.com/anyproto/anytype-heart/core/block/import"
	"github.com/anyproto/anytype-heart/core/block/object"
	"github.com/anyproto/anytype-heart/core/block/process"
	"github.com/anyproto/anytype-heart/core/block/restriction"
	"github.com/anyproto/anytype-heart/core/block/source"
	"github.com/anyproto/anytype-heart/core/configfetcher"
	"github.com/anyproto/anytype-heart/core/debug"
	"github.com/anyproto/anytype-heart/core/event"
	"github.com/anyproto/anytype-heart/core/history"
	"github.com/anyproto/anytype-heart/core/indexer"
	"github.com/anyproto/anytype-heart/core/kanban"
	"github.com/anyproto/anytype-heart/core/recordsbatcher"
	"github.com/anyproto/anytype-heart/core/relation"
	"github.com/anyproto/anytype-heart/core/session"
	"github.com/anyproto/anytype-heart/core/status"
	"github.com/anyproto/anytype-heart/core/subscription"
	"github.com/anyproto/anytype-heart/core/wallet"
	"github.com/anyproto/anytype-heart/metrics"
	"github.com/anyproto/anytype-heart/pkg/lib/cafe"
	"github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/datastore/clientds"
	"github.com/anyproto/anytype-heart/pkg/lib/files"
	"github.com/anyproto/anytype-heart/pkg/lib/gateway"
	"github.com/anyproto/anytype-heart/pkg/lib/ipfs/ipfslite"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/filestore"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/ftsearch"
	"github.com/anyproto/anytype-heart/pkg/lib/localstore/objectstore"
	"github.com/anyproto/anytype-heart/pkg/lib/pin"
	"github.com/anyproto/anytype-heart/pkg/lib/profilefinder"
	"github.com/anyproto/anytype-heart/pkg/lib/threads"
	walletUtil "github.com/anyproto/anytype-heart/pkg/lib/wallet"
	"github.com/anyproto/anytype-heart/util/builtinobjects"
	"github.com/anyproto/anytype-heart/util/builtintemplate"
	"github.com/anyproto/anytype-heart/util/linkpreview"
	"github.com/anyproto/anytype-heart/util/unsplash"
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
