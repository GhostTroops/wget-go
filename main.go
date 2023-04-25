package main

import (
	"flag"
	util "github.com/hktalent/go-utils"
	"github.com/hktalent/wget-go/pkg"
	"runtime"
	"sync"
)

func main() {
	util.DoInitAll()
	runtime.GOMAXPROCS(runtime.NumCPU())
	pkg.PipelineHttp1.SetNoLimit()

	//os.Args = []string{"", "-u", "https://huggingface.co/TencentARC/T2I-Adapter/resolve/main/models/t2iadapter_style_sd14v1.pth"}
	var t = flag.Bool("t", false, "file name with datetime")

	var workerCount = flag.Int64("c", 8, "Connection concurrency")
	var downloadUrl, out string
	flag.StringVar(&out, "o", "", "out file name")
	flag.StringVar(&downloadUrl, "u", "", "Download URL")
	flag.Parse()

	if "" != downloadUrl {
		pkg.Main(t, downloadUrl, out, workerCount, nil)
	} else {
		var out1 = make(chan *string)
		util.DoSyncFunc(func() {
			util.ReadStdIn(out1)
		})
		for {
			select {
			case s := <-out1:
				if nil == s {
					break
				} else if "" != *s {
					var wg sync.WaitGroup
					pkg.Main(t, *s, out, workerCount, &wg)
					wg.Wait()
				}
			}
		}
	}
	util.Wg.Wait()
	util.CloseAll()
}
