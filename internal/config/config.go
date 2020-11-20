package config

import (
	"github.com/kovetskiy/ko"
	"gopkg.in/yaml.v2"
)

type Database struct {
	DatabaseURI  string `yaml:"uri" required:"true" env:"DATABASE_URI"`
	DatabaseName string `yaml:"name" required:"true" env:"DATABASE_NAME"`
}

type Bitbucket struct {
	URL                       string `yaml:"url" required:"true"`
	Username                  string `yaml:"username" required:"true"`
	Password                  string `yaml:"password" required:"true"`
	Version                   string `yaml:"version" required:"true" env:"BITBUCKET_VERSION"`
	JvmSupportRecommendedArgs string `yaml:"jvm_support_recommended_args" required:"true" env:"JVM_SUPPORT_RECOMMENDED_ARGS"`
	ServerProxyName           string `yaml:"server_proxy_name" required:"true" env:"SERVER_PROXY_NAME"`
	ElasticSearchEnabled      string `yaml:"elastic_search_enabled" required:"true" env:"ELASTICSEARCH_ENABLED"`
}

type Config struct {
	Prefix        string    `yaml:"prefix" required:"true"`
	BaseURL       string    `yaml:"base_url" required:"true"`
	ListeningPort string    `yaml:"listening_port" required:"true"`
	Database      Database  `yaml:"database" required:"true"`
	Bitbucket     Bitbucket `yaml:"bitbucket" required:"true"`
}

func Load(path string) (*Config, error) {
	config := &Config{}
	err := ko.Load(path, config, ko.RequireFile(false), yaml.Unmarshal)
	if err != nil {
		return nil, err
	}

	return config, nil
}
