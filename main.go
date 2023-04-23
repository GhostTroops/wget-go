package main

import (
	"flag"
	"github.com/hktalent/wget-go/pkg"
)

func main() {
	//os.Args = []string{"", "-u", "https://huggingface.co/TencentARC/T2I-Adapter/resolve/main/models/t2iadapter_style_sd14v1.pth"}
	var t = flag.Bool("t", false, "file name with datetime")

	var workerCount = flag.Int64("c", 8, "Connection concurrency")
	var downloadUrl, out string
	flag.StringVar(&out, "o", "", "out file name")
	flag.StringVar(&downloadUrl, "u", "", "Download URL")
	flag.Parse()

	pkg.Main(t, downloadUrl, out, workerCount)
}
