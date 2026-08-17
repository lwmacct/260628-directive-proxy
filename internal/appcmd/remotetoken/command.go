package remotetoken

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
)

var Command = &cli.Command{
	Name:  "remote-token",
	Usage: "generate the fixed Relay dp.22.remote token",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "resolver-url", Usage: "Vendor resolver URL", Required: true},
	},
	Action: action,
}

func action(_ context.Context, command *cli.Command) error {
	hmacSecret := strings.TrimSpace(os.Getenv("DIRECTIVE_HMAC_SECRET"))
	resolverToken := strings.TrimSpace(os.Getenv("RELAY_ENTRY_S2S_TOKEN"))
	if hmacSecret == "" || resolverToken == "" {
		return errors.New("DIRECTIVE_HMAC_SECRET and RELAY_ENTRY_S2S_TOKEN are required")
	}
	token, err := Generate(hmacSecret, command.String("resolver-url"), resolverToken)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, token)
	return err
}
