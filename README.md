# sonyport

`sonyport` is an unofficial macOS CLI for importing photos and videos from a mounted Sony camera into date-based folders.

## Features

- Scans `/Volumes` for mounted storage that contains `DCIM`
- Prefers Sony-style layouts such as `PRIVATE/M4ROOT`, `AVF_INFO`, and `*MSDCF`
- Imports both photos and videos into `DESTINATION/YYYY-MM-DD/`
- Shows an import summary before execution, including per-date photo and video counts
- Shows scanning progress while building the import plan, with in-place updates on a real terminal
- Displays the current destination status and the previous destination used
- Asks for `yes/no` confirmation before importing by default
- Prints an English reminder to delete or format media on the camera itself after import
- Supports duplicate handling modes: `skip`, `rename`, and `overwrite`
- Uses `filetime` as the default date source for speed, with optional `mdls` mode
- Keeps file-by-file output off by default; use `--verbose` when needed
- Uses GitHub Releases + GoReleaser for Homebrew formula distribution through `marcy326/tap`

## Requirements

- macOS
- The camera must be mounted under `/Volumes`
- Media files must exist under `DCIM`

If the camera is connected in `PTP/MTP` mode and does not appear as mounted storage in Finder, this tool cannot read it directly. Switch the camera USB mode to `Mass Storage`.

## Installation

Install with Homebrew:

```bash
brew tap marcy326/tap https://github.com/marcy326/homebrew-tap
brew install marcy326/tap/sonyport
```

Uninstall:

```bash
brew uninstall marcy326/tap/sonyport
```

## Usage

Basic import:

```bash
sonyport ~/Pictures/CameraImport
```

Reuse the last destination:

```bash
sonyport
```

Specify the source volume explicitly:

```bash
sonyport --source /Volumes/SONY ~/Pictures/CameraImport
```

Skip the confirmation prompt:

```bash
sonyport --yes ~/Pictures/CameraImport
```

Dry run:

```bash
sonyport --dry-run ~/Pictures/CameraImport
```

Use Spotlight metadata dates instead of file timestamps:

```bash
sonyport --date-source mdls ~/Pictures/CameraImport
```

Duplicate handling:

```bash
sonyport --duplicate skip ~/Pictures/CameraImport
sonyport --duplicate rename ~/Pictures/CameraImport
sonyport --duplicate overwrite ~/Pictures/CameraImport
```

Show every planned/imported file:

```bash
sonyport --verbose --dry-run ~/Pictures/CameraImport
```

## Duplicate Modes

- `skip`: do not import if the destination file already exists
- `rename`: save as `filename_1.jpg`, `filename_2.mp4`, and so on
- `overwrite`: replace the existing destination file

The default mode is `skip`.

## Date Source

- `filetime`: uses the file modification time; this is the default because it is much faster
- `mdls`: uses Spotlight metadata via `mdls`; this can be slower on large cards

## Import Summary

Before the actual import starts, `sonyport` prints:

- Source path
- Destination path
- Duplicate mode
- Whether confirmation is interactive or skipped
- That source cleanup should be done on the camera itself
- Current destination status
- Previous destination information from the last successful import
- Per-date counts for planned photo imports, planned video imports, and duplicates

## State File

`sonyport` stores its reusable state, including the last destination path, at:

```text
~/Library/Application Support/sonyport/state.json
```

If you omit `DESTINATION_PATH`, `sonyport` uses the `last_destination` value from that file.

## License

MIT
