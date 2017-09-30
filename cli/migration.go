package cli

import (
	"github.com/evergreen-ci/evergreen"
	"github.com/evergreen-ci/evergreen/migrations"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
)

type MigrationCommand struct {
	ConfigPath string `long:"conf" default:"/etc/mci_settings.yml" description:"path to the service configuration file"`
	MongoDBURI string `long:"mongodburi" default:"" description:"alternate mongodb uri, override config file"`
	DryRun     bool   `long:"dry-run" short:"n" description:"run migration in a dry-run mode"`
}

const migrationFeatureDisabled = true

func (c *MigrationCommand) Execute(_ []string) error {
	if migrationFeatureDisabled {
		return errors.New("migrations are not enabled in this build")
	}

	settings, err := evergreen.NewSettings(c.ConfigPath)
	if err != nil {
		return errors.Wrap(err, "problem getting settings")
	}

	if err = settings.Validate(); err != nil {
		return errors.Wrap(err, "problem validating settings")
	}

	if c.MongoDBURI == "" {
		c.MongoDBURI = settings.Database.Url
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	env, err := migrations.Setup(ctx, c.MongoDBURI)
	if err != nil {
		return errors.Wrap(err, "problem setting up migration environment")
	}
	defer env.Close()

	app, err := migrations.Application(env)
	if err != nil {
		return errors.Wrap(err, "problem configuring migration application")
	}
	app.DryRun = c.DryRun
	return errors.Wrap(app.Run(ctx), "problem running migration operation")
}