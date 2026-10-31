package main

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// catalogsSheetCmd renders crops as numbered contact sheets so a reviewer can label or check
// many of them at a glance (the hand-labelled set behind `catalogs vision --eval` was built
// this way). Input: one crop per line, the path in the second tab-separated column (or the
// only column); paths are relative to DATA_DIR.
func catalogsSheetCmd() *cobra.Command {
	var list, out string
	var cell, cols, rows int
	cmd := &cobra.Command{
		Use:   "sheet",
		Short: "Render numbered contact sheets of crops listed in a TSV (for labelling and review)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			f, err := os.Open(list)
			if err != nil {
				return err
			}
			defer f.Close()
			var paths []string
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				cols := strings.Split(strings.TrimSpace(sc.Text()), "\t")
				if len(cols) == 0 || cols[0] == "" || strings.HasPrefix(cols[0], "#") {
					continue
				}
				p := cols[0]
				if len(cols) > 1 && strings.Contains(cols[1], "/") {
					p = cols[1]
				}
				paths = append(paths, p)
			}
			if err := os.MkdirAll(out, 0o755); err != nil {
				return err
			}
			per := cols * rows
			for s := 0; s*per < len(paths); s++ {
				sheet := image.NewRGBA(image.Rect(0, 0, cols*cell, rows*cell))
				draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.Gray{0xd0}), image.Point{}, draw.Src)
				for i := 0; i < per && s*per+i < len(paths); i++ {
					idx := s*per + i
					x0, y0 := (i%cols)*cell, (i/cols)*cell
					box := image.Rect(x0+1, y0+1, x0+cell-1, y0+cell-1)
					draw.Draw(sheet, box, image.White, image.Point{}, draw.Src)
					if img := loadImage(filepath.Join(cfg.DataDir, filepath.FromSlash(paths[idx]))); img != nil {
						b := img.Bounds()
						inner := cell - 24
						sc := min(float64(inner)/float64(b.Dx()), float64(inner)/float64(b.Dy()))
						w, h := max(1, int(float64(b.Dx())*sc)), max(1, int(float64(b.Dy())*sc))
						at := image.Pt(x0+(cell-w)/2, y0+18+(inner-h)/2)
						dst := image.Rectangle{Min: at, Max: at.Add(image.Pt(w, h))}
						xdraw.CatmullRom.Scale(sheet, dst, img, b, xdraw.Src, nil)
					}
					d := &font.Drawer{Dst: sheet, Src: image.NewUniform(color.RGBA{0xc0, 0, 0, 0xff}), Face: basicfont.Face7x13,
						Dot: fixed.P(x0+4, y0+13)}
					d.DrawString(fmt.Sprintf("%d", idx+1))
				}
				name := filepath.Join(out, fmt.Sprintf("sheet_%02d.png", s+1))
				w, err := os.Create(name)
				if err != nil {
					return err
				}
				if err := png.Encode(w, sheet); err != nil {
					w.Close()
					return err
				}
				w.Close()
				fmt.Printf("%s: crops %d–%d\n", name, s*per+1, min(len(paths), (s+1)*per))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&list, "list", "", "TSV with crop paths (first column, or second when the first is an id)")
	cmd.Flags().StringVar(&out, "out", "sheets", "output directory")
	cmd.Flags().IntVar(&cell, "cell", 200, "cell size in px")
	cmd.Flags().IntVar(&cols, "cols", 8, "columns per sheet")
	cmd.Flags().IntVar(&rows, "rows", 5, "rows per sheet")
	_ = cmd.MarkFlagRequired("list")
	return cmd
}

func loadImage(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	return img
}
