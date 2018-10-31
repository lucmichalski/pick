package commands

import (
	"bytes"
	builtinerrors "errors"
	"fmt"
	"os"

	"github.com/bndw/pick/backends"
	fileBackend "github.com/bndw/pick/backends/file"
	s3Backend "github.com/bndw/pick/backends/s3"
	"github.com/bndw/pick/crypto"
	"github.com/bndw/pick/errors"
	"github.com/bndw/pick/safe"
	"github.com/bndw/pick/utils"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func runCommand(c func([]string, *pflag.FlagSet) error, cmd *cobra.Command, args []string) {
	if err := c(args, cmd.Flags()); err != nil {
		if err == errors.ErrInvalidCommandUsage {
			_ = cmd.Usage()
			os.Exit(1)
		}
		os.Exit(handleError(err))
	}
	os.Exit(0)
}

func runMovedCommand(c func([]string, *pflag.FlagSet) error, cmd *cobra.Command, args []string, nl string) {
	red := color.New(color.FgRed).PrintfFunc()
	red(fmt.Sprintf("NOTE: This command has moved to %q and will be removed soon.\n\n", nl))
	runCommand(c, cmd, args)
}

type safeLoader struct {
	writable     bool
	password     *[]byte
	maxLoadTries int
	loadTries    int
}

func newSafeLoader(writable bool) *safeLoader {
	return &safeLoader{
		writable:     writable,
		maxLoadTries: 0,
	}
}

func (sl *safeLoader) RememberPassword() {
	sl.maxLoadTries++
}

func (sl *safeLoader) Load() (*safe.Safe, error) {
	backendClient, err := newBackendClient()
	if err != nil {
		return nil, err
	}
	if _, err := backendClient.Load(); err != nil {
		return nil, builtinerrors.New("pick not yet initialized. Please run the init command first")
	}
	return sl.LoadWithBackendClient(backendClient)
}

func (sl *safeLoader) LoadWithBackendClient(backendClient backends.Client) (*safe.Safe, error) {
	if err := backendClient.SetWritable(sl.writable); err == errors.ErrAlreadyRunning {
		return nil, err
	}

	if sl.password == nil {
		password, err := utils.GetPasswordInput(fmt.Sprintf("Enter your master password for safe '%s'", backendClient.SafeLocation()))
		if err != nil {
			return nil, err
		}
		sl.password = &password
	}

	cryptoClient, err := newCryptoClient()
	if err != nil {
		return nil, err
	}

	s, err := safe.Load(
		*sl.password,
		backendClient,
		cryptoClient,
		config,
	)
	if err != nil {
		if sl.maxLoadTries > sl.loadTries {
			// Reset stored password and load again — asking for a new password
			sl.password = nil
			sl.loadTries++
			return sl.LoadWithBackendClient(backendClient)
		}
		return nil, err
	}
	return s, nil
}

func readMasterPassConfirmed(n bool) ([]byte, error) {
	var add string
	if n {
		add = "new "
	}
	msg1 := fmt.Sprintf("Please set a %smaster password. This is the only password you need to remember", add)
	password, err := utils.GetPasswordInput(msg1)
	if err != nil {
		return nil, err
	}
	msg2 := fmt.Sprintf("Please confirm your %smaster password", add)
	passwordConfirm, err := utils.GetPasswordInput(msg2)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(password, passwordConfirm) {
		return nil, builtinerrors.New("Master passwords do not match")
	}
	return password, nil
}

func initSafe() error {
	backendClient, err := newBackendClient()
	if err != nil {
		return err
	}

	if _, err := backendClient.Load(); err == nil { // nolint: vetshadow
		return builtinerrors.New("pick was already initialized")
	}

	if err := backendClient.SetWritable(true); err == errors.ErrAlreadyRunning {
		return err
	}

	password, err := readMasterPassConfirmed(false)
	if err != nil {
		return err
	}

	cryptoClient, err := newCryptoClient()
	if err != nil {
		return err
	}

	s, err := safe.Load(
		password,
		backendClient,
		cryptoClient,
		config,
	)
	if err != nil {
		return err
	}

	if err := s.Init(); err != nil {
		return err
	}

	fmt.Println("pick initialized")
	return nil
}

func newBackendClient() (backends.Client, error) {
	return backends.New(&config.Storage)
}

func newCryptoClient() (crypto.Client, error) {
	return crypto.New(&config.Encryption)
}

func handleError(err error) int {
	fmt.Println(err)
	return 1
}

func init() {
	fileBackend.Register()
	s3Backend.Register()
}
