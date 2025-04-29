package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

type ConfigService struct{}

var DefaultConfig = GlobalSettings{
	Accounts: []Account{},
}

type Account struct {
	Name           string    `json:"name" koanf:"name"`
	Cookies        string    `json:"cookies" koanf:"cookies"`
	Proxy          string    `json:"proxy" koanf:"proxy"`
	TokenSK        string    `json:"tokenSk" koanf:"-"`
	Login          string    `json:"login" koanf:"-"`
	CoinBalance    string    `json:"coinBalance" koanf:"-"`
	RewardsJson    string    `json:"rewardsJson" koanf:"-"`
	LastAuth       time.Time `json:"-"  koanf:"-"`
	SignInInfoJson string    `json:"signInInfoJson" koanf:"-"`
}

type GlobalSettings struct {
	Accounts []Account `json:"accounts" koanf:"accounts"`
}

var GlobalSettingsConfig GlobalSettings
var GlobalSettingsPath string = "./config.yaml"

func (g *ConfigService) GetConfig(_ struct{}) GlobalSettings {
	configExists := Exists(GlobalSettingsPath)
	if !configExists {
		fmt.Println("Created a new user settings config")
		GlobalSettingsConfig = DefaultConfig
	}
	file, _ := os.ReadFile(GlobalSettingsPath)
	if len(file) == 0 {
		fmt.Println("config file is empty")
		GlobalSettingsConfig = DefaultConfig
	} else {
		GlobalSettingsConfig, _ = parseGlobalConfig()
	}

	log.Println("Config", GlobalSettingsConfig)
	return GlobalSettingsConfig
}

func (g *ConfigService) AddAccountToConfig(account Account) {
	log.Println("Config", account)
	account.Cookies = base64.StdEncoding.EncodeToString([]byte(account.Cookies))
	GlobalSettingsConfig.Accounts = append(GlobalSettingsConfig.Accounts, account)
	log.Println("GlobalSettingsConfig.Accounts", GlobalSettingsConfig.Accounts)
	SaveGlobalConfig()
}

func (g *ConfigService) RemoveAccountFromConfig(target_account Account) error {
	// Find and remove the account
	for i, account := range GlobalSettingsConfig.Accounts {
		if account.Cookies == target_account.Cookies {
			// Remove the account from the slice
			GlobalSettingsConfig.Accounts = append(GlobalSettingsConfig.Accounts[:i], GlobalSettingsConfig.Accounts[i+1:]...)
			// Save the updated config
			err := SaveGlobalConfig()
			if err != nil {
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("account not found")
}

func SaveGlobalConfig() error {
	k := koanf.New(".")

	err := k.Load(structs.Provider(GlobalSettingsConfig, "koanf"), nil)
	if err != nil {
		fmt.Println(err)
		return err
	}
	b, err := k.Marshal(yaml.Parser())
	if err != nil {
		fmt.Println(err)
		return err
	}

	err = os.WriteFile(GlobalSettingsPath, b, os.ModePerm)

	if err != nil {
		fmt.Println(err)
		return err
	}

	return nil
}

func parseGlobalConfig() (GlobalSettings, error) {
	var c GlobalSettings
	var k = koanf.New(".")
	if err := k.Load(file.Provider(GlobalSettingsPath), yaml.Parser()); err != nil {
		log.Printf("error loading global config: %v", err)
		return DefaultConfig, err
	}
	err := k.Unmarshal("", &c)
	if err != nil {
		log.Printf("error Unmarshaling global config: %v", err)
		return DefaultConfig, err
	}

	return c, nil
}
