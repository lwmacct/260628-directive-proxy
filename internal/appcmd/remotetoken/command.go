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
	Usage: "generate a dp.22.remote token for an HTTP resolver",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "resolver-url", Usage: "Vendor resolver URL", Required: true},
		&cli.StringFlag{Name: "resolver-token", Usage: "Vendor resolver Bearer token", Required: true},
		&cli.StringFlag{Name: "uuid", Usage: "optional stable RemoteSpec UUID"},
	},
	Action: action,
}

func action(_ context.Context, command *cli.Command) error {
	hmacSecret := strings.TrimSpace(os.Getenv("DIRECTIVE_HMAC_SECRET"))
	if hmacSecret == "" {
		return errors.New("DIRECTIVE_HMAC_SECRET is required")
	}
	token, err := Generate(hmacSecret, command.String("resolver-url"), command.String("resolver-token"), command.String("uuid"))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, token)
	return err
}
