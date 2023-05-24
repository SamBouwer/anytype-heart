package main

import (
	"context"
	"github.com/anyproto/anytype-heart/core/anytype"
	coreService "github.com/anyproto/anytype-heart/pkg/lib/core"
	"github.com/anyproto/anytype-heart/pkg/lib/profilefinder"
	walletUtil "github.com/anyproto/anytype-heart/pkg/lib/wallet"
	"github.com/anyproto/anytype-heart/util/console"
	"github.com/spf13/cobra"
)

var cafeCmd = &cobra.Command{
	Use:   "cafe",
	Short: "Cafe-specific commands",
}

var (
	mnemonic string
	account  string
)

var findProfiles = &cobra.Command{
	Use:   "findprofiles",
	Short: "Find profiles by mnemonic or accountId",
	Run: func(c *cobra.Command, args []string) {
		var (
			appMnemonic    string
			appAccount     walletUtil.Keypair
			accountsToFind []string
			err            error
		)

		if mnemonic != "" {
			for i := 0; i < 10; i++ {
				ac, err := coreService.WalletAccountAt(mnemonic, i, "")
				if err != nil {
					console.Fatal("failed to get account from provided mnemonic: %s", err.Error())
					return
				}

				accountsToFind = append(accountsToFind, ac.Address())
			}
		} else if account != "" {
			accountsToFind = []string{account}
		} else {
			console.Fatal("no mnemonic or account provided")
			return
		}
		// create temp walletUtil in order to do requests to cafe
		appMnemonic, err = coreService.WalletGenerateMnemonic(12)
		appAccount, err = coreService.WalletAccountAt(appMnemonic, 0, "")
		app, err := anytype.StartAccountRecoverApp(context.Background(), nil, appAccount)
		if err != nil {
			console.Fatal("failed to start anytype: %s", err.Error())
			return
		}
		var found bool
		var ch = make(chan coreService.Profile)
		closeCh := make(chan struct{})
		go func() {
			defer close(closeCh)
			select {
			case profile, ok := <-ch:
				if !ok {
					return
				}
				found = true
				console.Success("got profile: id=%s name=%s", profile.AccountAddr, profile.Name)
			}
		}()
		profileFinder := app.MustComponent(profilefinder.CName).(profilefinder.Service)
		err = profileFinder.FindProfilesByAccountIDs(context.Background(), accountsToFind, ch)
		if err != nil {
			console.Fatal("failed to query cafe: " + err.Error())
		}
		<-closeCh
		if !found {
			console.Fatal("no accounts found on cafe")
		}
	},
}

func init() {
	// subcommands
	cafeCmd.AddCommand(findProfiles)
	findProfiles.PersistentFlags().StringVarP(&mnemonic, "mnemonic", "", "", "mnemonic to find profiles on")
	findProfiles.PersistentFlags().StringVarP(&account, "account", "a", "", "account to find profiles on")
}
