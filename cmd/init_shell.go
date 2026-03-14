package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/paulbkim/agent-profile-manager/internal/shell"
)

var initCmd = &cobra.Command{
	Use:   "init <bash|zsh>",
	Short: "Output shell integration code",
	Long:  "Add 'eval \"$(apm init bash)\"' to your .bashrc or .zshrc",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("shell type required. Usage: apm init <bash|zsh>")
		}
		return cobra.ExactArgs(1)(cmd, args)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		shellType := args[0]
		script, err := shell.InitScript(shellType)
		if err != nil {
			return err
		}
		fmt.Print(script)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
