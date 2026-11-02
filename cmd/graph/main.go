package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"pmonitoring/graph"
)

// splitArgs отделяет флаги (в любом месте строки) от позиционных аргументов,
// т.к. пакет flag перестаёт парсить после первого позиционного аргумента.
func splitArgs(args []string) (flags, files []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-o" || a == "--out" || a == "-max-points" || a == "--max-points":
			flags = append(flags, a)
			if i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		case a == "-open" || a == "--open":
			flags = append(flags, a)
		case strings.HasPrefix(a, "--max-points=") ||
			strings.HasPrefix(a, "--out=") || strings.HasPrefix(a, "-o="):
			flags = append(flags, a)
		default:
			files = append(files, a)
		}
	}
	return
}

func main() {
	flags, files := splitArgs(os.Args[1:])
	fs := flag.NewFlagSet("graph", flag.ExitOnError)
	out := fs.String("o", "", "файл вывода (только для одного входного); расширение .png — растровый график")
	maxPoints := fs.Int("max-points", 4000, "прореживание точек при отрисовке")
	open := fs.Bool("open", false, "открыть результат в браузере (xdg-open)")
	if err := fs.Parse(flags); err != nil {
		os.Exit(2)
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "использование: graph <metrics_*.json> [файлы...] [-o out.svg|out.png] [--max-points N] [-open]")
		os.Exit(2)
	}
	if *out != "" && len(files) > 1 {
		fmt.Fprintln(os.Stderr, "-o можно указывать только с одним файлом")
		os.Exit(2)
	}
	outs := []string{}
	for _, f := range files {
		c, err := graph.Load(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
			continue
		}
		if *maxPoints > 0 && len(c.Ts()) > *maxPoints {
			c.Thin(len(c.Ts()) / *maxPoints)
		}
		dst := *out
		if dst == "" {
			dst = strings.TrimSuffix(f, filepath.Ext(f)) + ".svg"
		}
		var data []byte
		if strings.HasSuffix(strings.ToLower(dst), ".png") {
			data, err = c.RenderPNG()
		} else {
			data = []byte(c.RenderSVG())
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
			continue
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
			continue
		}
		fmt.Println("Сохранено:", dst)
		outs = append(outs, dst)
	}
	if *open && len(outs) > 0 {
		if err := exec.Command("xdg-open", outs[0]).Start(); err != nil {
			fmt.Fprintf(os.Stderr, "не удалось открыть: %v\n", err)
		}
	}
}
