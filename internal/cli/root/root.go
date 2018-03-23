package root

import (
	"github.com/alecthomas/kingpin"
	"github.com/apex/log"
	ooni "github.com/openobservatory/gooni"
	"github.com/openobservatory/gooni/internal/log/handlers/batch"
	"github.com/openobservatory/gooni/internal/log/handlers/cli"
	"github.com/prometheus/common/version"
)

// Cmd is the root command
var Cmd = kingpin.New("ooni", "")

// Command is syntax sugar for defining sub-commands
var Command = Cmd.Command

// Init should be called by all subcommand that care to have a ooni.OONI instance
var Init func() (*ooni.Context, error)

func init() {
	configPath := Cmd.Flag("config", "Set a custom config file path").Short('c').String()

	isVerbose := Cmd.Flag("verbose", "Enable verbose log output.").Short('v').Bool()
	isBatch := Cmd.Flag("batch", "Enable batch command line usage.").Bool()

	Cmd.PreAction(func(ctx *kingpin.ParseContext) error {
		if *isBatch {
			log.SetHandler(batch.Default)
		} else {
			log.SetHandler(cli.Default)
		}
		if *isVerbose {
			log.SetLevel(log.DebugLevel)
			log.Debugf("ooni version %s", version.Version)
		}

		Init = func() (*ooni.Context, error) {
			var err error

			homePath, err := ooni.GetOONIHome()
			if err != nil {
				return nil, err
			}

			ctx := ooni.NewContext(*configPath, homePath)
			err = ctx.Init()
			if err != nil {
				return nil, err
			}

			return ctx, nil
		}

		return nil
	})
}
