package options

type DocoptOptions struct {
	AddonPath   string `docopt:"--addonpath"`
	LicensePath string `docopt:"--licensepath"`
	Config      string `docopt:"--config"`
	Debug       bool   `docopt:"--debug"`
}
