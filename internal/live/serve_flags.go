package live

import "flag"

type serverFlagSet struct {
	port        int
	host        string
	noOpen      bool
	showVersion bool
	shareURL    string
	outputDir   string
	quiet       bool
	noIgnore    bool
	baseBranch  string
	vcsOverride string
	planDir     string
	planName    string
	fileArgs    []string
	prSpec      string
	rangeSpec   string
	scopeSpec   string
	remoteFiles bool
	liveOrigin  string
	liveCookie  string
	previewFile string
}

func parseServerFlags(args []string) serverFlagSet {
	fs := flag.NewFlagSet("crit", flag.ExitOnError)
	port := fs.Int("port", 0, "")
	fs.IntVar(port, "p", 0, "")
	host := fs.String("host", "", "")
	noOpen := fs.Bool("no-open", false, "")
	showVersion := fs.Bool("version", false, "")
	fs.BoolVar(showVersion, "v", false, "")
	shareURL := fs.String("share-url", "", "")
	outputDir := fs.String("output", "", "")
	fs.StringVar(outputDir, "o", "", "")
	quiet := fs.Bool("quiet", false, "")
	fs.BoolVar(quiet, "q", false, "")
	noIgnore := fs.Bool("no-ignore", false, "")
	baseBranch := fs.String("base-branch", "", "")
	vcsFlag := fs.String("vcs", "", "")
	planDir := fs.String("plan-dir", "", "")
	planName := fs.String("name", "", "")
	prSpec := fs.String("pr", "", "")
	rangeSpec := fs.String("range", "", "")
	scopeSpec := fs.String("scope", "", "")
	remoteFiles := fs.Bool("remote", false, "")
	liveOrigin := fs.String("live-origin", "", "")
	liveCookie := fs.String("live-cookie", "", "")
	previewFile := fs.String("preview-file", "", "")
	fs.Parse(args)

	return serverFlagSet{
		port:        *port,
		host:        *host,
		noOpen:      *noOpen,
		showVersion: *showVersion,
		shareURL:    *shareURL,
		outputDir:   *outputDir,
		quiet:       *quiet,
		noIgnore:    *noIgnore,
		baseBranch:  *baseBranch,
		vcsOverride: *vcsFlag,
		planDir:     *planDir,
		planName:    *planName,
		fileArgs:    fs.Args(),
		prSpec:      *prSpec,
		rangeSpec:   *rangeSpec,
		scopeSpec:   *scopeSpec,
		remoteFiles: *remoteFiles,
		liveOrigin:  *liveOrigin,
		liveCookie:  *liveCookie,
		previewFile: *previewFile,
	}
}
