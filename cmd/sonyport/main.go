package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

var version = "dev"

var copyData = io.Copy
var volumesRoot = "/Volumes"
var userConfigDir = os.UserConfigDir
var mdlsOutput = func(path string) ([]byte, error) {
	return exec.Command("mdls", "-raw", "-name", "kMDItemContentCreationDate", path).Output()
}

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

var photoExtensions = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
	".arw":  {},
	".hif":  {},
	".heif": {},
	".dng":  {},
	".tif":  {},
	".tiff": {},
	".png":  {},
}

var videoExtensions = map[string]struct{}{
	".mp4":  {},
	".mov":  {},
	".mts":  {},
	".m2ts": {},
}

var errCancelled = errors.New("cancelled")

type mediaType string

const (
	mediaUnknown mediaType = ""
	mediaPhoto   mediaType = "photo"
	mediaVideo   mediaType = "video"
)

type options struct {
	Source      string
	Destination string
	Duplicate   string
	DateSource  string
	DryRun      bool
	SkipConfirm bool
	Verbose     bool
	ShowHelp    bool
	ShowVersion bool
}

type plannedImport struct {
	SourcePath string
	TargetPath string
	Date       string
	Kind       mediaType
}

type dateSummary struct {
	Photos     int
	Videos     int
	Duplicates int
}

type scanSummary struct {
	Items           []plannedImport
	ByDate          map[string]*dateSummary
	FoundPhotos     int
	FoundVideos     int
	PlannedPhotos   int
	PlannedVideos   int
	FoundDuplicates int
	MdlsFallbacks   int
}

type candidateMedia struct {
	Path     string
	Kind     mediaType
	Date     string
	Modified time.Time
}

type progressRenderer struct {
	out          io.Writer
	interactive  bool
	lastLineLen  int
	lastFallback time.Time
}

type scanProgressReporter struct {
	renderer       *progressRenderer
	source         string
	startedAt      time.Time
	lastUpdate     time.Time
	updateEvery    time.Duration
	discovered     int
	photos         int
	videos         int
	totalToPlan    int
	currentPlanned int
}

type destinationInfo struct {
	Exists     bool
	DateDirs   int
	MediaFiles int
}

type lastImportState struct {
	Destination string `json:"destination"`
	Source      string `json:"source"`
	ImportedAt  string `json:"imported_at"`
	Photos      int    `json:"photos"`
	Videos      int    `json:"videos"`
}

type persistedState struct {
	LastDestination string           `json:"last_destination,omitempty"`
	LastSource      string           `json:"last_source,omitempty"`
	LastUsedAt      string           `json:"last_used_at,omitempty"`
	LastImport      *lastImportState `json:"last_import,omitempty"`

	// Legacy fields from the previous implementation.
	Destination string `json:"destination,omitempty"`
	Source      string `json:"source,omitempty"`
	ImportedAt  string `json:"imported_at,omitempty"`
	Photos      int    `json:"photos,omitempty"`
	Videos      int    `json:"videos,omitempty"`
}

func main() {
	exitCode := 0
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, errCancelled) {
			fmt.Fprintln(os.Stdout, "Cancelled.")
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			exitCode = 1
		}
	}
	os.Exit(exitCode)
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	opts, err := parseOptions(args, stdout, stderr)
	if err != nil {
		return err
	}

	if opts.ShowHelp {
		return nil
	}

	if opts.ShowVersion {
		fmt.Fprintln(stdout, version)
		return nil
	}

	state, err := loadState()
	if err != nil {
		return err
	}

	sourcePath := opts.Source
	if sourcePath == "" {
		sourcePath, err = detectCameraSource()
		if err != nil {
			return err
		}
	}

	sourcePath, err = filepath.Abs(sourcePath)
	if err != nil {
		return err
	}

	if err := ensureSourceLayout(sourcePath); err != nil {
		return err
	}

	destinationValue := opts.Destination
	if strings.TrimSpace(destinationValue) == "" {
		if state == nil || state.LastDestination == "" {
			printUsage(stdout)
			return errors.New("destination path is required on the first run; after that, sonyport can reuse the last destination")
		}
		destinationValue = state.LastDestination
		fmt.Fprintf(stderr, "Using previous destination: %s\n", destinationValue)
	}

	destinationPath, err := filepath.Abs(destinationValue)
	if err != nil {
		return err
	}

	renderer := newProgressRenderer(stderr)
	progress := newScanProgressReporter(renderer, sourcePath)
	candidates, err := collectSourceMedia(sourcePath, progress)
	if err != nil {
		return err
	}
	progress.FinishDiscovery()

	scan, err := buildPlan(candidates, destinationPath, opts.Duplicate, opts.DateSource, progress)
	if err != nil {
		return err
	}
	progress.FinishPlanning(scan)

	renderer.Finish(fmt.Sprintf("Inspecting destination: %s", destinationPath))
	destInfo, err := inspectDestination(destinationPath)
	if err != nil {
		return err
	}

	printSummary(stdout, sourcePath, destinationPath, opts, destInfo, state, scan)

	if scan.FoundPhotos == 0 && scan.FoundVideos == 0 {
		fmt.Fprintln(stdout, "\nNo supported photos or videos were found under DCIM.")
		return nil
	}

	if len(scan.Items) == 0 {
		fmt.Fprintln(stdout, "\nNothing to import after applying the duplicate rules.")
		return nil
	}

	if opts.DryRun {
		if opts.Verbose {
			printPlannedActions(stdout, scan.Items, true)
		}
		fmt.Fprintf(stdout, "\nDry run complete. Planned imports: %d\n", len(scan.Items))
		return nil
	}

	if !opts.SkipConfirm {
		proceed, err := confirmImport(stdin, stdout)
		if err != nil {
			return err
		}
		if !proceed {
			return errCancelled
		}
	}

	if err := os.MkdirAll(destinationPath, 0o755); err != nil {
		return err
	}

	importedPhotos := 0
	importedVideos := 0
	skipped := 0
	importProgress := newProgressRenderer(stderr)
	importProgress.Finish(fmt.Sprintf("Starting import: %d planned items", len(scan.Items)))

	for index, item := range scan.Items {
		targetPath, action, err := resolveTargetForExecution(item, opts.Duplicate)
		if err != nil {
			return err
		}

		if action == "skip" {
			if opts.Verbose {
				fmt.Fprintf(stdout, "Skip: %s\n", targetPath)
			} else {
				importProgress.Update(progressLine("Importing", index+1, len(scan.Items)))
			}
			skipped++
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		if err := copyFile(item.SourcePath, targetPath); err != nil {
			return err
		}

		if opts.Verbose {
			fmt.Fprintf(stdout, "Imported: %s -> %s\n", filepath.Base(item.SourcePath), targetPath)
		} else {
			importProgress.Update(progressLine("Importing", index+1, len(scan.Items)))
		}
		switch item.Kind {
		case mediaPhoto:
			importedPhotos++
		case mediaVideo:
			importedVideos++
		}
	}
	if !opts.Verbose {
		importProgress.Finish(progressLine("Importing", len(scan.Items), len(scan.Items)))
	}

	if state == nil {
		state = &persistedState{}
	}
	state.LastDestination = destinationPath
	state.LastSource = sourcePath
	state.LastUsedAt = time.Now().Format(time.RFC3339)
	state.LastImport = &lastImportState{
		Destination: destinationPath,
		Source:      sourcePath,
		ImportedAt:  time.Now().Format(time.RFC3339),
		Photos:      importedPhotos,
		Videos:      importedVideos,
	}
	if err := saveState(*state); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "\nImported photos: %d\n", importedPhotos)
	fmt.Fprintf(stdout, "Imported videos: %d\n", importedVideos)
	fmt.Fprintf(stdout, "Skipped during copy: %d\n", skipped)
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Delete the imported files on the camera itself after you verify the import.")
	fmt.Fprintln(stdout, "Do not delete them from the computer side.")
	return nil
}

func parseOptions(args []string, stdout, stderr io.Writer) (options, error) {
	var opts options

	fs := flag.NewFlagSet("sonyport", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var help bool
	var yes bool

	fs.StringVar(&opts.Source, "source", "", "source path")
	fs.StringVar(&opts.Duplicate, "duplicate", "skip", "duplicate handling mode")
	fs.StringVar(&opts.DateSource, "date-source", "filetime", "date source: filetime or mdls")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "preview import plan without copying")
	fs.BoolVar(&yes, "yes", false, "skip confirmation prompt")
	fs.BoolVar(&opts.SkipConfirm, "skip-confirm", false, "skip confirmation prompt")
	fs.BoolVar(&opts.Verbose, "verbose", false, "show per-file output")
	fs.BoolVar(&opts.ShowVersion, "version", false, "show version")
	fs.BoolVar(&help, "help", false, "show help")
	fs.BoolVar(&help, "h", false, "show help")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout)
			opts.ShowHelp = true
			return opts, nil
		}
		return opts, err
	}

	if help {
		printUsage(stdout)
		opts.ShowHelp = true
		return opts, nil
	}

	if opts.ShowVersion {
		return opts, nil
	}

	opts.SkipConfirm = opts.SkipConfirm || yes

	switch opts.Duplicate {
	case "skip", "rename", "overwrite":
	default:
		return opts, fmt.Errorf("--duplicate must be one of: skip, rename, overwrite")
	}

	switch opts.DateSource {
	case "filetime", "mdls":
	default:
		return opts, fmt.Errorf("--date-source must be one of: filetime, mdls")
	}

	remaining := fs.Args()
	if len(remaining) > 1 {
		printUsage(stdout)
		return opts, fmt.Errorf("at most one destination path may be provided")
	}
	if len(remaining) == 1 {
		opts.Destination = remaining[0]
	}
	return opts, nil
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  sonyport [--source CAMERA_PATH] [--duplicate MODE] [--date-source MODE] [--dry-run] [--yes|--skip-confirm] [--verbose] [DESTINATION_PATH]\n\n")
	fmt.Fprintf(w, "Options:\n")
	fmt.Fprintf(w, "  --source PATH         Import source root. Defaults to an auto-detected volume under /Volumes.\n")
	fmt.Fprintf(w, "  --duplicate MODE      Existing destination file handling: skip, rename, overwrite. Default: skip\n")
	fmt.Fprintf(w, "  --date-source MODE    Date source: filetime, mdls. Default: filetime\n")
	fmt.Fprintf(w, "  --dry-run             Show the import plan without copying files.\n")
	fmt.Fprintf(w, "  --yes                 Skip the confirmation prompt.\n")
	fmt.Fprintf(w, "  --skip-confirm        Skip the confirmation prompt.\n")
	fmt.Fprintf(w, "  --verbose             Show per-file output.\n")
	fmt.Fprintf(w, "  --version             Show the current version.\n")
	fmt.Fprintf(w, "  -h, --help            Show this help.\n\n")
	fmt.Fprintf(w, "Examples:\n")
	fmt.Fprintf(w, "  sonyport ~/Pictures/CameraImport\n")
	fmt.Fprintf(w, "  sonyport                 # reuse the last destination\n")
	fmt.Fprintf(w, "  sonyport --source /Volumes/SONY --duplicate skip ~/Pictures/CameraImport\n")
}

func ensureSourceLayout(source string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("source path is not a directory: %s", source)
	}

	dcimPath := filepath.Join(source, "DCIM")
	info, err = os.Stat(dcimPath)
	if err != nil {
		return fmt.Errorf("DCIM directory not found: %s", dcimPath)
	}
	if !info.IsDir() {
		return fmt.Errorf("DCIM is not a directory: %s", dcimPath)
	}
	return nil
}

func detectCameraSource() (string, error) {
	return detectCameraSourceFromRoot(volumesRoot)
}

func detectCameraSourceFromRoot(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}

	bestPath := ""
	bestScore := -1
	duplicateBest := false

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		volumePath := filepath.Join(root, entry.Name())
		score := sonyScoreForVolume(volumePath)
		if score < 0 {
			continue
		}

		if score > bestScore {
			bestPath = volumePath
			bestScore = score
			duplicateBest = false
		} else if score == bestScore {
			duplicateBest = true
		}
	}

	if bestPath == "" {
		return "", errors.New("could not detect a mounted camera source; use --source")
	}

	if duplicateBest {
		return "", errors.New("multiple equally likely camera sources found; use --source")
	}

	return bestPath, nil
}

func sonyScoreForVolume(volumePath string) int {
	if _, err := os.Stat(filepath.Join(volumePath, "DCIM")); err != nil {
		return -1
	}

	score := 0
	name := strings.ToLower(filepath.Base(volumePath))
	if strings.Contains(name, "sony") {
		score += 3
	}
	if isDir(filepath.Join(volumePath, "PRIVATE", "M4ROOT")) {
		score += 2
	}
	if isDir(filepath.Join(volumePath, "AVF_INFO")) {
		score++
	}

	dcimEntries, err := os.ReadDir(filepath.Join(volumePath, "DCIM"))
	if err == nil {
		for _, entry := range dcimEntries {
			if entry.IsDir() && strings.Contains(strings.ToUpper(entry.Name()), "MSDCF") {
				score += 2
				break
			}
		}
	}

	return score
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func collectSourceMedia(source string, progress *scanProgressReporter) ([]candidateMedia, error) {
	media := make([]candidateMedia, 0)
	dcimPath := filepath.Join(source, "DCIM")

	err := filepath.WalkDir(dcimPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		kind := detectMediaType(path)
		if kind == mediaUnknown {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		media = append(media, candidateMedia{
			Path:     path,
			Kind:     kind,
			Date:     info.ModTime().Format("2006-01-02"),
			Modified: info.ModTime(),
		})
		if progress != nil {
			progress.RecordDiscovery(kind)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return media, nil
}

func buildPlan(media []candidateMedia, destination, duplicateMode, dateSource string, progress *scanProgressReporter) (scanSummary, error) {
	summary := scanSummary{
		ByDate: make(map[string]*dateSummary),
	}
	reservedTargets := make(map[string]string)

	for _, item := range media {
		switch item.Kind {
		case mediaPhoto:
			summary.FoundPhotos++
		case mediaVideo:
			summary.FoundVideos++
		}
	}

	if progress != nil {
		progress.StartPlanning(len(media))
	}

	for index, item := range media {
		if progress != nil {
			progress.RecordPlanning(index + 1)
		}

		dateValue, usedFallback, err := determineDate(item, dateSource)
		if err != nil {
			return scanSummary{}, err
		}
		if usedFallback {
			summary.MdlsFallbacks++
		}

		targetPath := filepath.Join(destination, dateValue, filepath.Base(item.Path))
		targetPath, skipImport, err := resolvePlannedTarget(targetPath, item.Path, duplicateMode, reservedTargets)
		if err != nil {
			return scanSummary{}, err
		}
		if skipImport {
			day := ensureDateSummary(summary.ByDate, dateValue)
			day.Duplicates++
			summary.FoundDuplicates++
			continue
		}

		day := ensureDateSummary(summary.ByDate, dateValue)
		switch item.Kind {
		case mediaPhoto:
			summary.PlannedPhotos++
			day.Photos++
		case mediaVideo:
			summary.PlannedVideos++
			day.Videos++
		}

		summary.Items = append(summary.Items, plannedImport{
			SourcePath: item.Path,
			TargetPath: targetPath,
			Date:       dateValue,
			Kind:       item.Kind,
		})
		reservedTargets[targetPath] = item.Path
	}

	sort.Slice(summary.Items, func(i, j int) bool {
		if summary.Items[i].Date == summary.Items[j].Date {
			return summary.Items[i].SourcePath < summary.Items[j].SourcePath
		}
		return summary.Items[i].Date < summary.Items[j].Date
	})

	return summary, nil
}

func resolvePlannedTarget(targetPath, sourcePath, duplicateMode string, reservedTargets map[string]string) (string, bool, error) {
	if existingSource, ok := reservedTargets[targetPath]; ok {
		switch duplicateMode {
		case "skip":
			return "", true, nil
		case "rename":
			return nextAvailableReservedTarget(targetPath, reservedTargets), false, nil
		case "overwrite":
			return "", false, fmt.Errorf("multiple source files would overwrite the same target: %s and %s -> %s", existingSource, sourcePath, targetPath)
		default:
			return "", false, fmt.Errorf("unsupported duplicate mode: %s", duplicateMode)
		}
	}

	switch duplicateMode {
	case "skip":
		if fileExists(targetPath) {
			return "", true, nil
		}
	case "rename":
		if fileExists(targetPath) {
			return nextAvailableReservedTarget(targetPath, reservedTargets), false, nil
		}
	case "overwrite":
	default:
		return "", false, fmt.Errorf("unsupported duplicate mode: %s", duplicateMode)
	}

	return targetPath, false, nil
}

func newProgressRenderer(out io.Writer) *progressRenderer {
	renderer := &progressRenderer{out: out}
	file, ok := out.(*os.File)
	if !ok {
		return renderer
	}

	info, err := file.Stat()
	if err != nil {
		return renderer
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return renderer
	}
	term := os.Getenv("TERM")
	if term == "" || term == "dumb" {
		return renderer
	}
	renderer.interactive = true
	return renderer
}

func (r *progressRenderer) Update(line string) {
	if r == nil {
		return
	}
	if r.interactive {
		padding := ""
		if len(line) < r.lastLineLen {
			padding = strings.Repeat(" ", r.lastLineLen-len(line))
		}
		fmt.Fprintf(r.out, "\r%s%s", line, padding)
		r.lastLineLen = len(line)
		return
	}

	now := time.Now()
	if now.Sub(r.lastFallback) < time.Second {
		return
	}
	fmt.Fprintln(r.out, line)
	r.lastFallback = now
}

func (r *progressRenderer) Finish(line string) {
	if r == nil {
		return
	}
	if r.interactive {
		padding := ""
		if len(line) < r.lastLineLen {
			padding = strings.Repeat(" ", r.lastLineLen-len(line))
		}
		fmt.Fprintf(r.out, "\r%s%s\n", line, padding)
		r.lastLineLen = 0
		return
	}
	fmt.Fprintln(r.out, line)
	r.lastFallback = time.Now()
}

func newScanProgressReporter(renderer *progressRenderer, source string) *scanProgressReporter {
	interval := time.Second
	if renderer != nil && renderer.interactive {
		interval = 120 * time.Millisecond
	}

	reporter := &scanProgressReporter{
		renderer:    renderer,
		source:      filepath.Join(source, "DCIM"),
		startedAt:   time.Now(),
		lastUpdate:  time.Now(),
		updateEvery: interval,
	}
	reporter.renderer.Finish(fmt.Sprintf("Scanning source: %s", reporter.source))
	return reporter
}

func (r *scanProgressReporter) RecordDiscovery(kind mediaType) {
	r.discovered++
	switch kind {
	case mediaPhoto:
		r.photos++
	case mediaVideo:
		r.videos++
	}

	now := time.Now()
	if now.Sub(r.lastUpdate) < r.updateEvery {
		return
	}
	r.lastUpdate = now
	r.renderer.Update(fmt.Sprintf(
		"Scanning source: %s  found %d supported files (%d photos, %d videos)",
		r.source,
		r.discovered,
		r.photos,
		r.videos,
	))
}

func (r *scanProgressReporter) FinishDiscovery() {
	r.renderer.Finish(fmt.Sprintf(
		"Scanning complete: %d supported files found in %s (%d photos, %d videos)",
		r.discovered,
		r.source,
		r.photos,
		r.videos,
	))
}

func (r *scanProgressReporter) StartPlanning(total int) {
	r.totalToPlan = total
	r.currentPlanned = 0
	r.lastUpdate = time.Now()
	if total == 0 {
		return
	}
	r.renderer.Finish(fmt.Sprintf("Building import plan: 0/%d (0%%)", total))
}

func (r *scanProgressReporter) RecordPlanning(current int) {
	r.currentPlanned = current
	if r.totalToPlan == 0 {
		return
	}
	now := time.Now()
	if now.Sub(r.lastUpdate) < r.updateEvery && current < r.totalToPlan {
		return
	}
	r.lastUpdate = now
	percent := current * 100 / r.totalToPlan
	r.renderer.Update(fmt.Sprintf("Building import plan: %d/%d (%d%%)", current, r.totalToPlan, percent))
}

func (r *scanProgressReporter) FinishPlanning(summary scanSummary) {
	duration := time.Since(r.startedAt).Round(time.Millisecond)
	r.renderer.Finish(fmt.Sprintf(
		"Plan complete in %s: %d/%d processed (100%%), planned imports %d, found duplicates %d",
		duration,
		r.totalToPlan,
		r.totalToPlan,
		len(summary.Items),
		summary.FoundDuplicates,
	))
}

func ensureDateSummary(m map[string]*dateSummary, date string) *dateSummary {
	if existing, ok := m[date]; ok {
		return existing
	}
	created := &dateSummary{}
	m[date] = created
	return created
}

func detectMediaType(path string) mediaType {
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := photoExtensions[ext]; ok {
		return mediaPhoto
	}
	if _, ok := videoExtensions[ext]; ok {
		return mediaVideo
	}
	return mediaUnknown
}

func determineDate(item candidateMedia, dateSource string) (string, bool, error) {
	switch dateSource {
	case "filetime":
		return item.Date, false, nil
	case "mdls":
		return dateForFileUsingMdls(item)
	default:
		return "", false, fmt.Errorf("unsupported date source: %s", dateSource)
	}
}

func dateForFileUsingMdls(item candidateMedia) (string, bool, error) {
	path := item.Path
	output, err := mdlsOutput(path)
	if err == nil {
		raw := strings.TrimSpace(string(output))
		if raw != "" && raw != "(null)" {
			dateValue := strings.Fields(raw)
			if len(dateValue) > 0 && datePattern.MatchString(dateValue[0]) {
				return dateValue[0], false, nil
			}
		}
	}

	if !item.Modified.IsZero() {
		return item.Modified.Format("2006-01-02"), true, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", false, err
	}
	return info.ModTime().Format("2006-01-02"), true, nil
}

func nextAvailableTarget(path string) string {
	if !fileExists(path) {
		return path
	}

	extension := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), extension)
	dir := filepath.Dir(path)

	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, i, extension))
		if !fileExists(candidate) {
			return candidate
		}
	}
}

func nextAvailableReservedTarget(path string, reservedTargets map[string]string) string {
	if !fileExists(path) {
		if _, ok := reservedTargets[path]; !ok {
			return path
		}
	}

	extension := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), extension)
	dir := filepath.Dir(path)

	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, i, extension))
		if fileExists(candidate) {
			continue
		}
		if _, ok := reservedTargets[candidate]; ok {
			continue
		}
		return candidate
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func inspectDestination(path string) (destinationInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return destinationInfo{}, nil
		}
		return destinationInfo{}, err
	}
	if !info.IsDir() {
		return destinationInfo{}, fmt.Errorf("destination path is not a directory: %s", path)
	}

	result := destinationInfo{Exists: true}

	entries, err := os.ReadDir(path)
	if err != nil {
		return destinationInfo{}, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !datePattern.MatchString(entry.Name()) {
			continue
		}
		result.DateDirs++

		dateEntries, err := os.ReadDir(filepath.Join(path, entry.Name()))
		if err != nil {
			return destinationInfo{}, err
		}
		for _, dateEntry := range dateEntries {
			if dateEntry.IsDir() {
				continue
			}
			if detectMediaType(dateEntry.Name()) != mediaUnknown {
				result.MediaFiles++
			}
		}
	}

	return result, nil
}

func loadState() (*persistedState, error) {
	statePath, err := stateFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, nil
	}

	if state.LastDestination == "" && state.Destination != "" {
		state.LastDestination = state.Destination
		state.LastSource = state.Source
		state.LastUsedAt = state.ImportedAt
		state.LastImport = &lastImportState{
			Destination: state.Destination,
			Source:      state.Source,
			ImportedAt:  state.ImportedAt,
			Photos:      state.Photos,
			Videos:      state.Videos,
		}
	}
	return &state, nil
}

func saveState(state persistedState) error {
	statePath, err := stateFilePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(statePath, data, 0o644)
}

func stateFilePath() (string, error) {
	configDir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "sonyport", "state.json"), nil
}

func printSummary(w io.Writer, source, destination string, opts options, destInfo destinationInfo, state *persistedState, scan scanSummary) {
	fmt.Fprintln(w, "Import Summary")
	fmt.Fprintln(w, "--------------")
	fmt.Fprintf(w, "Source: %s\n", source)
	fmt.Fprintf(w, "Destination: %s\n", destination)
	fmt.Fprintf(w, "Duplicate mode: %s\n", opts.Duplicate)
	fmt.Fprintf(w, "Date source: %s\n", opts.DateSource)
	fmt.Fprintf(w, "Confirmation: %s\n", confirmationMode(opts))
	fmt.Fprintln(w, "Source cleanup: delete on the camera")
	fmt.Fprintf(w, "Verbose file output: %s\n", yesNo(opts.Verbose))
	if opts.DryRun {
		fmt.Fprintln(w, "Mode: dry-run")
	}

	fmt.Fprintln(w, "\nCurrent destination")
	fmt.Fprintf(w, "  Exists: %s\n", yesNo(destInfo.Exists))
	fmt.Fprintf(w, "  Date folders: %d\n", destInfo.DateDirs)
	fmt.Fprintf(w, "  Media files: %d\n", destInfo.MediaFiles)

	fmt.Fprintln(w, "\nPrevious destination")
	if state == nil || state.LastDestination == "" {
		fmt.Fprintln(w, "  No previous destination recorded.")
	} else {
		fmt.Fprintf(w, "  Path: %s\n", state.LastDestination)
		fmt.Fprintf(w, "  Source: %s\n", state.LastSource)
		fmt.Fprintf(w, "  Last used at: %s\n", state.LastUsedAt)
	}

	fmt.Fprintln(w, "\nPrevious successful import")
	if state == nil || state.LastImport == nil {
		fmt.Fprintln(w, "  No previous successful import recorded.")
	} else {
		fmt.Fprintf(w, "  Path: %s\n", state.LastImport.Destination)
		fmt.Fprintf(w, "  Source: %s\n", state.LastImport.Source)
		fmt.Fprintf(w, "  Imported at: %s\n", state.LastImport.ImportedAt)
		fmt.Fprintf(w, "  Photos: %d\n", state.LastImport.Photos)
		fmt.Fprintf(w, "  Videos: %d\n", state.LastImport.Videos)
	}

	fmt.Fprintln(w, "\nPlanned imports by date")
	tabW := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tabW, "DATE\tPHOTOS\tVIDEOS\tDUPLICATES")
	dates := make([]string, 0, len(scan.ByDate))
	for dateValue := range scan.ByDate {
		dates = append(dates, dateValue)
	}
	sort.Strings(dates)
	for _, dateValue := range dates {
		item := scan.ByDate[dateValue]
		fmt.Fprintf(tabW, "%s\t%d\t%d\t%d\n", dateValue, item.Photos, item.Videos, item.Duplicates)
	}
	tabW.Flush()

	fmt.Fprintln(w, "\nTotals")
	fmt.Fprintf(w, "  Found photos: %d\n", scan.FoundPhotos)
	fmt.Fprintf(w, "  Found videos: %d\n", scan.FoundVideos)
	fmt.Fprintf(w, "  Planned photos: %d\n", scan.PlannedPhotos)
	fmt.Fprintf(w, "  Planned videos: %d\n", scan.PlannedVideos)
	fmt.Fprintf(w, "  Found duplicates: %d\n", scan.FoundDuplicates)
	if scan.MdlsFallbacks > 0 {
		fmt.Fprintf(w, "  Metadata fallbacks to filetime: %d\n", scan.MdlsFallbacks)
	}
}

func printPlannedActions(w io.Writer, items []plannedImport, dryRun bool) {
	label := "Import"
	if dryRun {
		label = "Would import"
	}

	for _, item := range items {
		fmt.Fprintf(w, "%s: %s -> %s\n", label, filepath.Base(item.SourcePath), item.TargetPath)
	}
}

func progressLine(label string, current, total int) string {
	if total == 0 {
		return fmt.Sprintf("%s: 0/0 (0%%)", label)
	}
	percent := current * 100 / total
	return fmt.Sprintf("%s: %d/%d (%d%%)", label, current, total, percent)
}

func confirmationMode(opts options) string {
	if opts.SkipConfirm || opts.DryRun {
		return "skipped"
	}
	return "interactive"
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func confirmImport(stdin io.Reader, stdout io.Writer) (bool, error) {
	fmt.Fprint(stdout, "\nProceed with import? [y/N]: ")
	reader := bufio.NewReader(stdin)
	response, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}

	answer := strings.ToLower(strings.TrimSpace(response))
	return answer == "y" || answer == "yes", nil
}

func resolveTargetForExecution(item plannedImport, duplicateMode string) (string, string, error) {
	targetPath := item.TargetPath
	exists := fileExists(targetPath)

	switch duplicateMode {
	case "skip":
		if exists {
			return targetPath, "skip", nil
		}
	case "rename":
		if exists {
			targetPath = nextAvailableTarget(targetPath)
		}
	case "overwrite":
	default:
		return "", "", fmt.Errorf("unsupported duplicate mode: %s", duplicateMode)
	}

	return targetPath, "import", nil
}

func copyFile(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	info, err := source.Stat()
	if err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(filepath.Dir(targetPath), "."+filepath.Base(targetPath)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if err := tempFile.Chmod(info.Mode().Perm()); err != nil {
		tempFile.Close()
		return err
	}

	if _, err := copyData(tempFile, source); err != nil {
		tempFile.Close()
		return err
	}

	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		return err
	}

	if err := tempFile.Close(); err != nil {
		return err
	}

	if err := os.Chtimes(tempPath, info.ModTime(), info.ModTime()); err != nil {
		return err
	}

	if err := os.Rename(tempPath, targetPath); err != nil {
		return err
	}

	cleanupTemp = false
	return nil
}
