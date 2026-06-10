#!/usr/bin/env node
/**
 * Generates internal CLI handler files from cmd/crit/main.go during layout refactor.
 * Run from repo root: node scripts/gen_cli_layout.js
 */
const fs = require('fs');
const path = require('path');

const root = path.join(__dirname, '..');
const mainPath = path.join(root, 'cmd/crit/main.go');
const lines = fs.readFileSync(mainPath, 'utf8').split('\n');

function extract(start, end) {
  return lines.slice(start - 1, end).join('\n');
}

function applyReplacements(body, reps) {
  let s = body;
  for (const [from, to] of reps) {
    s = s.split(from).join(to);
  }
  return s;
}

function exitToError(body) {
  let s = body;
  // Functions that should return error
  s = s.replace(/func run(\w+)\(args \[\]string\) \{\s*\/\/nolint:[^\n]+\n/g, 'func Run$1(args []string) error { //nolint:gocyclo\n');
  s = s.replace(/func run(\w+)\(args \[\]string\) \{/g, 'func Run$1(args []string) error {');
  s = s.replace(/func run(\w+)\(\) \{/g, 'func Run$1() error {');
  s = s.replace(/func run(\w+)\(([^)]*)\) \{/g, (m, name, params) => {
    if (name === 'SharePreview' || name === 'ShareExisting' || name === 'ShareNew') return m;
    return `func run${name}(${params}) {`;
  });

  // Specific error returns
  s = s.replace(/fmt\.Fprintf\(os\.Stderr, "Error: %v\\n", ([^\)]+)\)\s*\n\s*os\.Exit\(1\)/g, 'return $1');
  s = s.replace(/fmt\.Fprintln\(os\.Stderr, "Error: "\+([^\.]+)\.Error\(\)\)\s*\n\s*os\.Exit\(1\)/g, 'return $1');
  s = s.replace(/fmt\.Fprintln\(os\.Stderr, "Error: "\+err\.Error\(\)\)\s*\n\s*os\.Exit\(1\)/g, 'return err');

  // Usage exits -> return Usage error
  s = s.replace(/fmt\.Fprintln\(os\.Stderr, "Usage: ([^"]+)"\)\s*\n(?:\s*fmt\.Fprintln\(os\.Stderr,[^\n]+\)\s*\n)*\s*os\.Exit\(1\)/g,
    (m, usage) => `return clicmd.Usage("Usage: ${usage}")`);

  s = s.replace(/printShareUsage\(\)/g, 'return shareUsageError()');
  s = s.replace(/printCommentUsage\(\)/g, 'return commentUsageError()');

  // Bare os.Exit(1)
  s = s.replace(/\n\s*os\.Exit\(1\)/g, '\n\t\treturn clicmd.ExitError{Code: 1, Err: errors.New("exit")}');

  // Push live exit code
  s = s.replace(/if code := runPushLive\(ctx, b\); code != 0 \{\s*\n\s*os\.Exit\(code\)\s*\n\s*\}/g,
    'if code := RunPushLive(ctx, b); code != 0 {\n\t\treturn clicmd.ExitError{Code: code, Err: errors.New("push failed")}\n\t}');

  // Early returns in void functions -> return nil
  s = s.replace(/(\n\treturn\n)/g, '\n\treturn nil\n');
  s = s.replace(/(\n\treturn\s*)\n(\t\})/g, '\n\treturn nil\n$2');

  // runPushLive -> RunPushLive for export
  s = s.replace(/func runPushLive/g, 'func RunPushLive');
  s = s.replace(/redirectReviewPathForPR/g, 'RedirectReviewPathForPR');

  return s;
}

// --- share ---
let shareBody = extract(57, 549);
shareBody = applyReplacements(shareBody, [
  ['crawlPreview(', 'session.CrawlPreview('],
  ['defaultShareURL', 'config.DefaultShareURL'],
  ['resolveReviewPath(', 'review.ResolveReviewPath('],
  ['resolveReviewPathWithArgs(', 'review.ResolveReviewPathWithArgs('],
  ['needsShareConsent(', 'config.NeedsShareConsent('],
  ['saveGlobalConfig(', 'config.SaveGlobalConfig('],
  ['mustGetwd()', 'mustGetwd()'],
  ['shareFile', 'ShareFile'],
  ['webComment', 'WebComment'],
  ['loadShareConfig()', 'loadShareConfig()'],
  ['fetchWebComments(', 'fetchWebComments('],
  ['mergeWebComments(', 'MergeWebComments('],
  ['upsertShareToWeb(', 'UpsertShareToWeb('],
  ['updateShareState(', 'UpdateShareState('],
  ['computeShareHash(', 'ComputeShareHash('],
  ['buildLocalIDSet(', 'BuildLocalIDSet('],
  ['buildLocalFingerprintIndex(', 'BuildLocalFingerprintIndex('],
  ['persistShareState(', 'PersistShareState('],
  ['clearShareState(', 'ClearShareState('],
  ['loadCommentsForShare(', 'loadCommentsForShare('],
  ['shareReviewFiles(', 'ShareReviewFiles('],
  ['buildSharePayload(', 'BuildSharePayload('],
  ['setBearer(', 'SetBearer('],
  ['decodeJSONOrHTMLHint(', 'DecodeJSONOrHTMLHint('],
  ['reviewPathsFor(', 'session.ReviewPathsFor('],
  ['readFileShared(', 'session.ReadFileShared('],
  ['CritJSON', 'session.CritJSON'],
  ['runSharePreview(', 'runSharePreview('],
  ['runShareExisting(', 'runShareExisting('],
  ['runShareNew(', 'runShareNew('],
]);
shareBody = exitToError(shareBody);
shareBody = shareBody.replace(/func runSharePreview\(sf shareFlags\) \{/g, 'func runSharePreview(sf shareFlags) error {');
shareBody = shareBody.replace(/func runShareExisting\(/g, 'func runShareExisting(');
shareBody = shareBody.replace(/func runShareNew\(/g, 'func runShareNew(');

const shareFile = `package share

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mdp/qrterminal/v3"
	"github.com/tomasz-tomczyk/crit/internal/auth"
	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"golang.org/x/term"
)

${shareBody.replace(/func printShareUsage\(\) \{[\s\S]*?os\.Exit\(1\)\n\}/, `func shareUsageError() error {
	fmt.Fprintln(os.Stderr, "Usage: crit share [--output <dir>] [--share-url <url>] [--org <slug>] [--visibility <level>] [--qr] <file> [file...]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Shares files to crit-web and prints the review URL.")
	fmt.Fprintln(os.Stderr, "Comments from the review file are included automatically.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Examples:")
	fmt.Fprintln(os.Stderr, "  crit share plan.md")
	fmt.Fprintln(os.Stderr, "  crit share plan.md src/main.go")
	fmt.Fprintln(os.Stderr, "  crit share --qr plan.md")
	return clicmd.Usage("invalid share usage")
}`)}

func mustGetwd() string {
	wd, err := clicmd.MustGetwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return wd
}
`;

fs.writeFileSync(path.join(root, 'internal/share/cli.go'), shareFile);

console.log('Wrote internal/share/cli.go');
