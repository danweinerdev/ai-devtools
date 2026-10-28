package main

import (
	"os"

	"github.com/spf13/cobra"
)

// DefaultHolder holds claims when neither --holder nor AID_HOLDER names one: a
// lone agent never types a holder, while delegated workers pass their role,
// so one holder at a time still holds.
const DefaultHolder = "main"

// holderFlag registers --holder on cmd; holderOf resolves its value.
func holderFlag(cmd *cobra.Command, holder *string, what string) {
	cmd.Flags().StringVar(holder, "holder", "", what+` (default: $AID_HOLDER, else "`+DefaultHolder+`")`)
}

// holderOf is the explicit --holder, else $AID_HOLDER, else DefaultHolder.
func holderOf(flag string) string {
	if flag != "" {
		return flag
	}
	if env := os.Getenv("AID_HOLDER"); env != "" {
		return env
	}
	return DefaultHolder
}
