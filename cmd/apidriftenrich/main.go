// Command apidriftenrich is the deterministic half of the API drift enrichment
// workflow. It reduces a canary report to validated identifiers, builds the context
// handed to the analysis job, and checks that job's output before anything uses it.
//
// Everything it reads from an issue, an upstream checkout or the analysis job is
// untrusted, so it parses to a strict shape and fails closed.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fatal(fmt.Errorf("usage: apidriftenrich gate|refs|bundle|materialize|verify|staged [flags]"))
	}
	var err error
	switch cmd, args := os.Args[1], os.Args[2:]; cmd {
	case "gate":
		err = runGate(args)
	case "refs":
		err = runRefs(args)
	case "bundle":
		err = runBundle(args)
	case "materialize":
		err = runMaterialize(args)
	case "verify":
		err = runVerify(args)
	case "staged":
		err = runStaged(args)
	default:
		err = fmt.Errorf("unknown subcommand %q", cmd)
	}
	if err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "apidriftenrich:", err)
	os.Exit(1)
}

func runGate(args []string) error {
	fs := flag.NewFlagSet("gate", flag.ContinueOnError)
	issuePath := fs.String("issue", "", "issue JSON")
	commentsPath := fs.String("comments", "", "newline-delimited comment JSON")
	out := fs.String("out", "", "findings JSON to write")
	if err := fs.Parse(args); err != nil {
		return err
	}
	issue, err := os.ReadFile(*issuePath)
	if err != nil {
		return err
	}
	comments, err := os.ReadFile(*commentsPath)
	if err != nil {
		return err
	}
	report, err := TrustedReport(issue, comments)
	if err != nil {
		return err
	}
	findings, err := ParseReport(report)
	if err != nil {
		return err
	}
	return writeJSON(*out, findings)
}

func runRefs(args []string) error {
	fs := flag.NewFlagSet("refs", flag.ContinueOnError)
	path := fs.String("findings", "", "findings JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	findings, err := readFindings(*path)
	if err != nil {
		return err
	}
	for _, ref := range findings.Refs {
		fmt.Println(ref)
	}
	return nil
}

func runBundle(args []string) error {
	fs := flag.NewFlagSet("bundle", flag.ContinueOnError)
	path := fs.String("findings", "", "findings JSON")
	upstream := fs.String("upstream", "", "directory holding <ref>/core and <ref>/plugins")
	repo := fs.String("repo", ".", "repository root")
	out := fs.String("out", "", "context JSON to write")
	if err := fs.Parse(args); err != nil {
		return err
	}
	findings, err := readFindings(*path)
	if err != nil {
		return err
	}
	context, err := BuildContext(findings, *upstream, *repo)
	if err != nil {
		return err
	}
	return os.WriteFile(*out, context, 0o600)
}

func runMaterialize(args []string) error {
	fs := flag.NewFlagSet("materialize", flag.ContinueOnError)
	output := fs.String("output", "", "structured analysis output")
	repo := fs.String("repo", "", "clean checkout to diff against")
	out := fs.String("out", "", "artifact directory to create")
	if err := fs.Parse(args); err != nil {
		return err
	}
	raw, err := os.ReadFile(*output)
	if err != nil {
		return err
	}
	result, err := Materialize(raw, *repo, *out)
	if err != nil {
		return err
	}
	fmt.Printf("comment_sha256=%s\npatch_sha256=%s\nhas_patch=%t\n", result.CommentSHA, result.PatchSHA, result.PatchSHA != "")
	return nil
}

func runVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	dir := fs.String("dir", "", "downloaded artifact directory")
	commentSHA := fs.String("comment-sha256", "", "expected comment digest")
	patchSHA := fs.String("patch-sha256", "", "expected patch digest, empty when there is no patch")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := VerifyArtifact(*dir, *commentSHA, *patchSHA); err != nil {
		return err
	}
	fmt.Printf("has_patch=%t\n", *patchSHA != "")
	return nil
}

func runStaged(args []string) error {
	fs := flag.NewFlagSet("staged", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return CheckStaged(*repo)
}
