package wallet

import (
	"fmt"
	"github.com/anyproto/anytype-heart/app"
	"github.com/anyproto/anytype-heart/metrics"
	"github.com/anyproto/anytype-heart/pkg/lib/logging"
	walletUtil "github.com/anyproto/anytype-heart/pkg/lib/wallet"
	"io/ioutil"
	"path/filepath"
)

const (
	CName          = "wallet"
	KeyFileAccount = "account.key"
	KeyFileDevice  = "device.key"
)

type wallet struct {
	rootPath       string
	repoPath       string // other components will init their files/dirs inside
	accountKeyPath string
	deviceKeyPath  string

	accountKeypair walletUtil.Keypair
	deviceKeypair  walletUtil.Keypair
}

func (r *wallet) GetAccountPrivkey() (walletUtil.Keypair, error) {
	if r.accountKeypair == nil {
		return nil, fmt.Errorf("not set")
	}
	return r.accountKeypair, nil
}

func (r *wallet) GetDevicePrivkey() (walletUtil.Keypair, error) {
	if r.deviceKeypair == nil {
		return nil, fmt.Errorf("not set")
	}
	return r.deviceKeypair, nil
}

func (r *wallet) Init(a *app.App) (err error) {
	var b []byte
	if r.deviceKeypair == nil && r.deviceKeyPath != "" {
		b, err = ioutil.ReadFile(r.deviceKeyPath)
		if err != nil {
			return fmt.Errorf("failed to read device keyfile: %w", err)
		}

		r.deviceKeypair, err = walletUtil.UnmarshalBinary(b)
		if err != nil {
			return err
		}

		if r.deviceKeypair.KeypairType() != walletUtil.KeypairTypeDevice {
			return fmt.Errorf("got %s key type instead of %s", r.deviceKeypair.KeypairType(), walletUtil.KeypairTypeDevice)
		}
	}

	if r.accountKeypair == nil && r.accountKeyPath != "" {
		b, err = ioutil.ReadFile(r.accountKeyPath)
		if err != nil {
			return fmt.Errorf("failed to read account keyfile: %w", err)
		}

		r.accountKeypair, err = walletUtil.UnmarshalBinary(b)
		if err != nil {
			return err
		}
		if r.accountKeypair.KeypairType() != walletUtil.KeypairTypeAccount {
			return fmt.Errorf("got %s key type instead of %s", r.accountKeypair.KeypairType(), walletUtil.KeypairTypeAccount)
		}
	}

	if r.deviceKeypair != nil {
		logging.SetHost(r.deviceKeypair.Address())
		metrics.SharedClient.SetDeviceId(r.deviceKeypair.Address())
	}
	if r.accountKeypair != nil {
		logging.SetAccount(r.accountKeypair.Address())
		metrics.SharedClient.SetUserId(r.accountKeypair.Address())
	}
	return nil
}

func (r *wallet) RepoPath() string {
	return r.repoPath
}

func (r *wallet) RootPath() string {
	return r.rootPath
}

func (r *wallet) Name() (name string) {
	return CName
}

func (r *wallet) Close() (err error) {
	return nil
}

func NewWithAccountRepo(rootpath, accountId string) Wallet {
	repoPath := filepath.Join(rootpath, accountId)
	return &wallet{
		rootPath:       rootpath,
		repoPath:       repoPath,
		accountKeyPath: filepath.Join(repoPath, KeyFileAccount),
		deviceKeyPath:  filepath.Join(repoPath, KeyFileDevice),
	}
}

func NewWithRepoPathAndKeys(repoPath string, accountKeypair, deviceKeypair walletUtil.Keypair) Wallet {
	return &wallet{
		repoPath:       repoPath,
		accountKeypair: accountKeypair,
		deviceKeypair:  deviceKeypair,
	}
}

type Wallet interface {
	RootPath() string
	RepoPath() string
	GetAccountPrivkey() (walletUtil.Keypair, error)
	GetDevicePrivkey() (walletUtil.Keypair, error)
	app.Component
}
