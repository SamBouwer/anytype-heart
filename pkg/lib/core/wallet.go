package core

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/anyproto/anytype-heart/pkg/lib/wallet"
	"github.com/libp2p/go-libp2p/core/crypto"
)

var ErrRepoExists = fmt.Errorf("repo not empty, reinitializing would overwrite your account")
var ErrRepoCorrupted = fmt.Errorf("repo is corrupted")

func WalletGenerateMnemonic(wordCount int) (string, error) {
	w, err := wallet.WalletFromWordCount(wordCount)
	if err != nil {
		return "", err
	}
	return w.RecoveryPhrase, nil
}

func WalletAccountAt(mnemonic string, index int, passphrase string) (wallet.Keypair, error) {
	w := wallet.WalletFromMnemonic(mnemonic)
	return w.AccountAt(index, passphrase)
}

func WalletInitRepo(rootPath string, seed []byte) error {
	pk, err := crypto.UnmarshalEd25519PrivateKey(seed)
	if err != nil {
		return err
	}

	accountKP, err := wallet.NewKeypairFromPrivKey(wallet.KeypairTypeAccount, pk)
	if err != nil {
		return err
	}

	accountKPBinary, err := accountKP.MarshalBinary()
	if err != nil {
		return err
	}

	deviceKP, err := wallet.NewRandomKeypair(wallet.KeypairTypeDevice)
	if err != nil {
		return err
	}

	deviceKPBinary, err := deviceKP.MarshalBinary()
	if err != nil {
		return err
	}

	repoPath := filepath.Join(rootPath, accountKP.Address())
	_, err = os.Stat(repoPath)
	if !os.IsNotExist(err) {
		return ErrRepoExists
	}

	os.MkdirAll(repoPath, 0700)
	accountKeyPath := filepath.Join(repoPath, "account.key")
	deviceKeyPath := filepath.Join(repoPath, "device.key")

	if err = ioutil.WriteFile(accountKeyPath, accountKPBinary, 0400); err != nil {
		return err
	}

	if err = ioutil.WriteFile(deviceKeyPath, deviceKPBinary, 0400); err != nil {
		return err
	}

	return nil
}
