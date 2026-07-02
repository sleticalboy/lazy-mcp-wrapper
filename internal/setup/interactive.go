package setup

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func shouldApply(opts Options, question string) bool {
	if opts.YesAll {
		return true
	}
	if opts.DryRun {
		return false
	}
	return askConfirm(question)
}

func askConfirm(question string) bool {
	fmt.Printf("%s [Y/n] ", question)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "" || answer == "y" || answer == "yes"
}
