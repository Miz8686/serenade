package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dhowden/tag"
)

// Track is one indexed audio file. Metadata comes from tags at index
// time (lazy per-file reads during the single startup walk); playback
// position/state always comes live from the backend.
type Track struct {
	Path   string
	Artist string
	Title  string
	Album  string
	TrackN int
}

func (t Track) label() string {
	if t.Artist != "" {
		return t.Artist + " – " + t.Title
	}
	return t.Title
}

// libraryDirs mirrors the curated sources. No cmus library involved.
func libraryDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, "FLAC"),
		filepath.Join(home, "Sparsha FLACS"),
		filepath.Join(home, "Phone-backup", "davinci-20260925", "4a", "FLAC"),
	}
}

func audioExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".flac", ".m4a", ".mp3", ".ogg", ".opus", ".wav":
		return true
	}
	return false
}

func readTrack(path string) Track {
	t := Track{Path: path, Title: strings.TrimSuffix(baseName(path), filepath.Ext(path))}
	f, err := os.Open(path)
	if err != nil {
		return t
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil {
		return t
	}
	if m.Title() != "" {
		t.Title = m.Title()
	}
	t.Artist = m.Artist()
	if t.Artist == "" {
		t.Artist = m.AlbumArtist()
	}
	t.Album = m.Album()
	if n, _ := m.Track(); n > 0 {
		t.TrackN = n
	}
	return t
}

// indexLibrary walks the sources; callers run it async and feed
// tracks back as a tea.Msg.
func indexLibrary() ([]Track, error) {
	var out []Track
	for _, dir := range libraryDirs() {
		_ = filepath.WalkDir(dir, func(path string, e os.DirEntry, err error) error {
			if err != nil || e.IsDir() || !audioExt(e.Name()) {
				return nil
			}
			out = append(out, readTrack(path))
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool {
		ai, aj := strings.ToLower(out[i].Artist), strings.ToLower(out[j].Artist)
		if ai != aj {
			return ai < aj
		}
		if out[i].Album != out[j].Album {
			return out[i].Album < out[j].Album
		}
		if out[i].TrackN != out[j].TrackN {
			return out[i].TrackN < out[j].TrackN
		}
		return out[i].Title < out[j].Title
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("no audio files found in library dirs")
	}
	return out, nil
}
