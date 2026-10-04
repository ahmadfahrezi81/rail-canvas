// Command cli: one-off admin tasks. Same image as the API: /app/cli <command>.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"golang.org/x/term"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/platform"
	"github.com/ahmadfahrezi81/rail-canvas/api/internal/service"
)

const usage = `usage: cli <command> [flags]

commands:
  bootstrap   create the first space and its owner (once; refuses if users exist)`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "bootstrap":
		err = bootstrap(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q\n\n%s", os.Args[1], usage)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func bootstrap(args []string) error {
	fs := flag.NewFlagSet("bootstrap", flag.ExitOnError)
	spaceName := fs.String("space-name", "", "space name, e.g. \"Pixel Club\"")
	spaceSlug := fs.String("space-slug", "", "lowercase letters, digits, dashes, e.g. pixel-club")
	email := fs.String("email", "", "owner's email")
	name := fs.String("name", "", "owner's display name")
	_ = fs.Parse(args)
	if *spaceName == "" || *spaceSlug == "" || *email == "" || *name == "" {
		fs.Usage()
		return errors.New("all flags are required")
	}

	// Typed at a prompt, never a flag: flags end up in shell history.
	password, err := readPassword()
	if err != nil {
		return err
	}

	ctx := context.Background()
	pool, err := platform.NewPostgres(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()

	spaceID, err := service.NewAuth(pool).Bootstrap(ctx, service.BootstrapParams{
		SpaceName: *spaceName, SpaceSlug: *spaceSlug, Email: *email, DisplayName: *name, Password: string(password),
	})
	if err != nil {
		return err
	}
	fmt.Printf("created space %q (%s) with owner %s\n", *spaceName, spaceID, service.NormalizeEmail(*email))
	return nil
}

func readPassword() ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, errors.New("run this in a terminal: the password is read from a prompt")
	}
	fmt.Fprint(os.Stderr, "Password (10 to 72 characters): ")
	p1, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, err
	}
	fmt.Fprint(os.Stderr, "Again: ")
	p2, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, err
	}
	switch {
	case !bytes.Equal(p1, p2):
		return nil, errors.New("passwords do not match")
	case len(p1) < 10 || len(p1) > 72:
		return nil, errors.New("password must be 10 to 72 characters")
	}
	return p1, nil
}
