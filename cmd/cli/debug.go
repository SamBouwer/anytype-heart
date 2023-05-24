package main

import (
	"context"
	"github.com/anyproto/anytype-heart/app"
	"github.com/anyproto/anytype-heart/core/anytype"
	"github.com/anyproto/anytype-heart/core/debug"
	"github.com/anyproto/anytype-heart/core/event"
	"github.com/anyproto/anytype-heart/pb"
	"github.com/anyproto/anytype-heart/util/console"
	"github.com/spf13/cobra"
	"os"
)

var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Debug commands",
}

var (
	debugRepoPath   string
	debugAccount    string
	debugThread     string
	debugOutputFile string
)

var dumpTree = &cobra.Command{
	Use:   "dump-tree",
	Short: "Dumps tree of changes for specific thread",
	Run: func(c *cobra.Command, args []string) {
		if debugAccount == "" {
			console.Fatal("please specify account")
		}
		if debugThread == "" {
			console.Fatal("please specify thread")
		}

		comps := []app.Component{
			anytype.BootstrapConfig(false, false),
			anytype.BootstrapWallet(debugRepoPath, debugAccount),
			event.NewCallbackSender(func(event *pb.Event) {}),
		}

		app, err := anytype.StartNewApp(context.Background(), comps...)
		if err != nil {
			console.Fatal("failed to start anytype: %s", err.Error())
		}

		dbg := app.MustComponent(debug.CName).(debug.Debug)

		isAnonymize := false
		dumpWithSvg := false
		filename, err := dbg.DumpTree(debugThread, debugOutputFile, isAnonymize, dumpWithSvg)
		if err != nil {
			console.Fatal("failed to dump tree: %s", err.Error())
		}
		console.Success("file saved: %s", filename)
	},
}
var dumpLocalstore = &cobra.Command{
	Use:   "dump-localstore",
	Short: "Dumps localstore for all objects",
	Run: func(c *cobra.Command, args []string) {
		if debugAccount == "" {
			console.Fatal("please specify account")
		}

		comps := []app.Component{
			anytype.BootstrapConfig(false, false),
			anytype.BootstrapWallet(debugRepoPath, debugAccount),
			event.NewCallbackSender(func(event *pb.Event) {}),
		}

		app, err := anytype.StartNewApp(context.Background(), comps...)
		if err != nil {
			console.Fatal("failed to start anytype: %s", err.Error())
		}

		dbg := app.MustComponent(debug.CName).(debug.Debug)

		filename, err := dbg.DumpLocalstore(nil, debugOutputFile)
		if err != nil {
			console.Fatal("failed to dump localstore: %s", err.Error())
		}
		console.Success("file saved: %s", filename)
	},
}

func init() {
	// subcommands
	homeDir, _ := os.UserHomeDir()

	debugCmd.AddCommand(dumpTree)
	debugCmd.AddCommand(dumpLocalstore)

	debugCmd.PersistentFlags().StringVarP(&debugRepoPath, "repo", "r", homeDir+"/.config/anytype2/data", "path to dir with accounts folder")
	debugCmd.PersistentFlags().StringVarP(&debugAccount, "account", "a", "", "id of account in the repo folder")
	debugCmd.PersistentFlags().StringVarP(&debugThread, "thread", "t", "", "id of thread to debug")
	debugCmd.PersistentFlags().StringVarP(&debugOutputFile, "out", "o", "./", "folder to save file")
}
