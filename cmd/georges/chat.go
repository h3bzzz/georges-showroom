package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"georges/internal/chat"
	"georges/internal/store"
)

// chatCmd talks to the assistant from the terminal: one question, or an interactive session
// when no argument is given. The same code path the browser uses, so tool behaviour can be
// checked without a browser.
func chatCmd() *cobra.Command {
	var rep bool
	var image, page string
	c := &cobra.Command{
		Use:   "chat [question]",
		Short: "Ask the showroom assistant (interactive without a question)",
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			svc := chat.New(cfg.AnthropicAPIKey, cfg.ChatModel, store.New(pool))
			if !svc.Enabled() {
				return chat.ErrDisabled
			}
			if m, err := openMatcher(cmd.Context(), pool); err == nil {
				svc.Matcher = m
			} else {
				fmt.Fprintln(os.Stderr, "matcher off:", err)
			}
			var history []chat.Message
			ask := func(q string) error {
				req := chat.Request{Rep: rep, Page: page}
				history = append(history, chat.Message{Role: "user", Content: q})
				req.Messages = history
				if image != "" {
					b, err := os.ReadFile(image)
					if err != nil {
						return err
					}
					req.Image = base64.StdEncoding.EncodeToString(b)
					switch strings.ToLower(filepath.Ext(image)) {
					case ".png":
						req.ImageType = "image/png"
					case ".webp":
						req.ImageType = "image/webp"
					default:
						req.ImageType = "image/jpeg"
					}
					image = "" // one picture per session
				}
				var text strings.Builder
				var shown []chat.Shown
				err := svc.Run(cmd.Context(), req, func(e chat.Event) {
					switch e.Type {
					case "status":
						fmt.Fprintf(os.Stderr, "  · %s\n", e.Text)
					case "delta":
						fmt.Print(e.Text)
						text.WriteString(e.Text)
					case "cards":
						for _, c := range e.Cards {
							fmt.Fprintf(os.Stderr, "  [card] %d %s %s %s %s\n", c.ID, c.Brand, c.Title, c.Price, c.URL)
						}
					case "error":
						fmt.Fprintln(os.Stderr, "error:", e.Error)
					case "done":
						shown = e.Shown
						fmt.Println()
					}
				})
				history = append(history, chat.Message{Role: "assistant", Content: text.String(), Shown: shown})
				return err
			}
			if len(args) > 0 {
				return ask(strings.Join(args, " "))
			}
			sc := bufio.NewScanner(os.Stdin)
			fmt.Print("you> ")
			for sc.Scan() {
				q := strings.TrimSpace(sc.Text())
				if q == "" {
					fmt.Print("you> ")
					continue
				}
				if err := ask(q); err != nil {
					return err
				}
				fmt.Print("\nyou> ")
			}
			return nil
		},
	}
	tool := &cobra.Command{
		Use:    "tool NAME JSON",
		Short:  "Run one assistant tool directly (debugging)",
		Args:   cobra.ExactArgs(2),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			pool, err := openDB(cmd.Context(), 4)
			if err != nil {
				return err
			}
			defer pool.Close()
			svc := chat.New("x", "", store.New(pool))
			if m, err := openMatcher(cmd.Context(), pool); err == nil {
				svc.Matcher = m
			}
			out, cards, isErr := svc.Tool(cmd.Context(), args[0], args[1], rep)
			for _, c := range cards {
				fmt.Fprintf(os.Stderr, "  [card] %d %s %s %s %s\n", c.ID, c.Brand, c.Title, c.Price, c.Image)
			}
			if isErr {
				fmt.Fprintln(os.Stderr, "tool error")
			}
			fmt.Println(out)
			return nil
		},
	}
	tool.Flags().BoolVar(&rep, "rep", false, "trade mode")
	c.AddCommand(tool)
	c.Flags().BoolVar(&rep, "rep", false, "trade mode: list price, UPC, lead time")
	c.Flags().StringVar(&image, "image", "", "attach a photograph to the first question")
	c.Flags().StringVar(&page, "page", "", "pretend the visitor is on this site path (e.g. /p/123)")
	return c
}
