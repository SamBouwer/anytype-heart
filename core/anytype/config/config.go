package config

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"github.com/anyproto/any-sync/app"
	"github.com/anyproto/any-sync/commonspace"
	"github.com/anyproto/any-sync/metric"
	commonnet "github.com/anyproto/any-sync/net"
	"github.com/anyproto/any-sync/nodeconf"
	"github.com/kelseyhightower/envconfig"
	"gopkg.in/yaml.v2"

	"github.com/anyproto/anytype-heart/core/wallet"
	"github.com/anyproto/anytype-heart/metrics"
	"github.com/anyproto/anytype-heart/pkg/lib/datastore/clientds"
	"github.com/anyproto/anytype-heart/pkg/lib/logging"
)

var log = logging.Logger("anytype-config")

const (
	CName = "config"
)

type FileConfig interface {
	GetFileConfig() (ConfigRequired, error)
	WriteFileConfig(cfg ConfigRequired) (ConfigRequired, error)
}

type ConfigRequired struct {
	HostAddr            string `json:",omitempty"`
	CustomFileStorePath string `json:",omitempty"`
	TimeZone            string `json:",omitempty"`
	LegacyFileStorePath string `json:",omitempty"`
}

type Config struct {
	ConfigRequired           `json:",inline"`
	NewAccount               bool `ignored:"true"` // set to true if a new account is creating. This option controls whether mw should wait for the existing data to arrive before creating the new log
	Offline                  bool
	DisableThreadsSyncEvents bool

	RepoPath    string
	AnalyticsId string

	DebugAddr       string
	LocalServerAddr string

	DS                     clientds.Config
	FS                     FSConfig
	DisableFileConfig      bool `ignored:"true"` // set in order to skip reading/writing config from/to file
	CreateBuiltinTemplates bool
}

type FSConfig struct {
	IPFSStorageAddr string
}

type DebugAPIConfig struct {
	commonnet.Config
	IsEnabled bool
}

const (
	ConfigFileName = "config.json"
)

var DefaultConfig = Config{
	Offline: false,

	LocalServerAddr: ":0",
	DS:              clientds.DefaultConfig,
}

func WithNewAccount(isNewAccount bool) func(*Config) {
	return func(c *Config) {
		c.NewAccount = isNewAccount
		if isNewAccount {
			c.AnalyticsId = metrics.GenerateAnalyticsId()
		}
	}
}

func WithDebugAddr(addr string) func(*Config) {
	return func(c *Config) {
		c.DebugAddr = addr
	}
}

func WithLocalServer(addr string) func(*Config) {
	return func(c *Config) {
		c.LocalServerAddr = addr
	}
}

func WithCreateBuiltinTemplates(createBuiltinTemplates bool) func(*Config) {
	return func(c *Config) {
		c.CreateBuiltinTemplates = createBuiltinTemplates
	}
}

func DisableFileConfig(disable bool) func(*Config) {
	return func(c *Config) {
		c.DisableFileConfig = disable
	}
}

func New(options ...func(*Config)) *Config {
	cfg := DefaultConfig
	for _, opt := range options {
		opt(&cfg)
	}
	return &cfg
}

func (c *Config) Init(a *app.App) (err error) {
	repoPath := a.MustComponent(wallet.CName).(wallet.Wallet).RepoPath()
	if err = c.initFromFileAndEnv(repoPath); err != nil {
		return
	}

	return
}

func (c *Config) initFromFileAndEnv(repoPath string) error {
	if repoPath == "" {
		return fmt.Errorf("repo is missing")
	}
	c.RepoPath = repoPath

	if !c.DisableFileConfig {
		var confRequired ConfigRequired
		err := GetFileConfig(c.GetConfigPath(), &confRequired)
		if err != nil {
			return fmt.Errorf("failed to get config from file: %s", err.Error())
		}

		writeConfig := func() error {
			err = WriteJsonConfig(c.GetConfigPath(), c.ConfigRequired)
			if err != nil {
				return fmt.Errorf("failed to save required configuration to the cfg file: %s", err.Error())
			}
			return nil
		}

		// Do not overwrite the legacy file store path from file if it's already set in memory
		if confRequired.LegacyFileStorePath == "" && c.LegacyFileStorePath != "" {
			confRequired.LegacyFileStorePath = c.LegacyFileStorePath
			c.ConfigRequired = confRequired
			if err := writeConfig(); err != nil {
				return err
			}
		}
		c.ConfigRequired = confRequired

		saveRandomHostAddr := func() error {
			port, err := getRandomPort()
			if err != nil {
				port = 4006
				log.Errorf("failed to get random port for gateway, go with the default %d: %s", port, err.Error())
			}

			c.HostAddr = fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)
			return writeConfig()
		}

		if c.HostAddr == "" {
			err = saveRandomHostAddr()
			if err != nil {
				return err
			}
		} else {
			parts := strings.Split(c.HostAddr, "/")
			if len(parts) == 0 {
				log.Errorf("failed to parse cfg.HostAddr: %s", c.HostAddr)
			} else {
				// lets test the existing port in config
				addr, err := net.ResolveTCPAddr("tcp4", "0.0.0.0:"+parts[len(parts)-1])
				if err == nil {
					l, err := net.ListenTCP("tcp4", addr)
					if err != nil {
						// the port from config is no longer available. It may be used by other app or blocked by the OS(e.g. port exclusion range on windows)
						// lets find another available port and save it to config
						err = saveRandomHostAddr()
						if err != nil {
							return err
						}
					} else {
						_ = l.Close()
					}
				}
			}
		}

	}

	err := envconfig.Process("ANYTYPE", c)
	if err != nil {
		log.Errorf("failed to read config from env: %v", err)
	}

	return nil
}

func (c *Config) Name() (name string) {
	return CName
}

func (c *Config) DSConfig() clientds.Config {
	return c.DS
}

func (c *Config) FSConfig() (FSConfig, error) {
	res := ConfigRequired{}
	err := GetFileConfig(c.GetConfigPath(), &res)
	if err != nil {
		return FSConfig{}, err
	}

	return FSConfig{IPFSStorageAddr: res.CustomFileStorePath}, nil
}

func (c *Config) GetConfigPath() string {
	return filepath.Join(c.RepoPath, ConfigFileName)
}

func getRandomPort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "0.0.0.0:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}

	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func (c *Config) GetSpace() commonspace.Config {
	return commonspace.Config{
		GCTTL:                60,
		SyncPeriod:           20,
		KeepTreeDataInMemory: true,
	}
}

func (c *Config) GetMetric() metric.Config {
	return metric.Config{}
}

func (c *Config) GetNet() commonnet.Config {
	return commonnet.Config{
		Server: commonnet.ServerConfig{
			ListenAddrs: []string{c.LocalServerAddr},
		},
		Stream: commonnet.StreamConfig{
			TimeoutMilliseconds: 1000,
			MaxMsgSizeMb:        256,
		},
	}
}

func (c *Config) GetDebugAPIConfig() DebugAPIConfig {
	return DebugAPIConfig{
		Config: commonnet.Config{
			Server: commonnet.ServerConfig{
				ListenAddrs: []string{c.DebugAddr},
			},
			Stream: commonnet.StreamConfig{
				TimeoutMilliseconds: 1000,
				MaxMsgSizeMb:        256,
			},
		},
		IsEnabled: len(c.DebugAddr) != 0,
	}
}

func (c *Config) GetNodeConf() (conf nodeconf.Configuration) {
	if err := yaml.Unmarshal(nodesConfYmlBytes, &conf); err != nil {
		panic(fmt.Errorf("unable to parse node config: %v", err))
	}
	return
}

func (c *Config) GetNodeConfStorePath() string {
	return filepath.Join(c.RepoPath, "nodeconf")
}
