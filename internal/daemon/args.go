package daemon

// ParseGitHookArgs extracts --repo and --event from the --git-hook arg list.
func ParseGitHookArgs(args []string) (repo, event string) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--repo":
			if i+1 < len(args) {
				repo = args[i+1]
				i++
			}
		case "--event":
			if i+1 < len(args) {
				event = args[i+1]
				i++
			}
		}
	}
	return repo, event
}
