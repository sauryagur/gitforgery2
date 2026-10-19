package main

import (
	"fmt"
	"os"

	"github.com/sauryagur/gitforgery2/internal/gitx"
	"github.com/sauryagur/gitforgery2/internal/tui"
)

func main() {
	repo := "."
	if len(os.Args) > 1 {
		repo = os.Args[1]
	}
	root, err := gitx.RepoRoot(repo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "not a git repo:", err)
		os.Exit(1)
	}
	if err := tui.Run(root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
