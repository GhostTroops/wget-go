package pkg

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cheggaaa/pb"
	"github.com/hktalent/PipelineHttp"
)

type Worker struct {
	Url       string
	File      *os.File
	Count     int64
	SyncWG    sync.WaitGroup
	TotalSize int64
	Progress
}

type Progress struct {
	Pool *pb.Pool
	Bars []*pb.ProgressBar
}

var (
	PipelineHttp1 = PipelineHttp.NewPipelineHttp()
	sCurDir, err  = os.Getwd()
)

func Main(t *bool, downloadUrl, out string, workerCount *int64, wg *sync.WaitGroup) {
	// Get header from the url
	log.Println("Url:", downloadUrl)
	szOldUrl := downloadUrl
	szFileName, s2, fileSize, err := getSizeAndCheckRangeSupport(downloadUrl)
	if nil != err {
		*workerCount = 1
	}
	if "" != s2 {
		downloadUrl = s2
	}
	log.Printf("File size: %d bytes, workerCount %d\n", fileSize, *workerCount)

	var filePath string
	if *t {
		filePath = sCurDir + string(filepath.Separator) + strconv.FormatInt(time.Now().UnixNano(), 10) + "_" + getFileName(downloadUrl)
	} else {
		if "" != out {
			filePath = sCurDir + string(filepath.Separator) + out
		} else if "" != szFileName {
			filePath = sCurDir + string(filepath.Separator) + szFileName
		} else {
			filePath = sCurDir + string(filepath.Separator) + getFileName(szOldUrl)
		}
	}
	log.Printf("Local path: %s\n", filePath)

	// 这里后期需要优化，当异常后第二次运行，从断点开始的情况 os.O_APPEND|
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0666)
	handleError(err)
	defer f.Close()

	// New worker struct to download file
	var worker = Worker{
		Url:       downloadUrl,
		File:      f,
		Count:     *workerCount,
		TotalSize: fileSize,
	}
	if nil != wg {
		worker.SyncWG = *wg
	}

	var start, end, partialSize int64
	if 0 < fileSize%(*workerCount) {
		partialSize = fileSize / (*workerCount - 1)
	} else {
		partialSize = fileSize / *workerCount
	}
	now := time.Now().UTC()
	for num := int64(0); num < worker.Count; num++ {
		// New sub progress bar (give it 0 at first for new instance and assign real size later on.)
		bar := pb.New(0).Prefix(fmt.Sprintf("Part %d  0%% ", num+1))
		bar.ShowSpeed = true
		bar.SetMaxWidth(100)
		bar.SetUnits(pb.U_BYTES_DEC)
		bar.SetRefreshRate(time.Second)
		bar.ShowPercent = true
		worker.Progress.Bars = append(worker.Progress.Bars, bar)

		if num == worker.Count {
			end = fileSize // last part
		} else {
			end = start + partialSize
			if end > fileSize {
				end = fileSize
			}
		}

		worker.SyncWG.Add(1)
		go worker.writeRange(num, start, end-1)
		start = end
	}
	worker.Progress.Pool, err = pb.StartPool(worker.Progress.Bars...)
	handleError(err)
	worker.SyncWG.Wait()
	worker.Progress.Pool.Stop()
	log.Println("Elapsed time:", time.Since(now))
	log.Println("Done!")
}

func (w *Worker) writeRange(partNum int64, start int64, end int64) {
	var written int64

	defer w.Bars[partNum].Finish()
	defer w.SyncWG.Done()
	if start >= end {
		return
	}
	body, size, err := w.getRangeBody(start, end)
	if err != nil {
		log.Fatalf("Part %d request error: %s\n", partNum+1, err.Error())
	}
	defer body.Close()

	// Assign total size to progress bar
	w.Bars[partNum].Total = size

	// New percentage flag
	percentFlag := map[int64]bool{}

	// make a buffer to keep chunks that are read
	buf := make([]byte, 8*1024)
	for {
		nr, er := body.Read(buf)
		if nr > 0 {
			nw, err := w.File.WriteAt(buf[0:nr], start)
			if err != nil {
				log.Fatalf("Part %d occured error: %s.\n", partNum+1, err.Error())
			}
			if nr != nw {
				log.Fatalf("Part %d occured error of short writiing.\n", partNum+1)
			}

			start = int64(nw) + start
			if nw > 0 {
				written += int64(nw)
			}

			// Update written bytes on progress bar
			w.Bars[int(partNum)].Set64(written)

			// Update current percentage on progress bars
			p := int64(float32(written) / float32(size) * 100)
			_, flagged := percentFlag[p]
			if !flagged {
				percentFlag[p] = true
				//w.Bars[int(partNum)].Prefix(fmt.Sprintf("Part %d(%d - %d)  %d%% ", partNum+1, start, end+1, p))
				w.Bars[int(partNum)].Prefix(fmt.Sprintf("Part %d %d%% ", partNum+1, p))
			}
		}
		if er != nil {
			if er.Error() == "EOF" {
				if size == written {
					// Download successfully
				} else {
					handleError(errors.New(fmt.Sprintf("Part %d unfinished.\n", partNum+1)))
				}
				break
			}
			handleError(errors.New(fmt.Sprintf("Part %d occured error: %s\n", partNum+1, er.Error())))
		}
	}
}

func (w *Worker) getRangeBody(start int64, end int64) (io.ReadCloser, int64, error) {
	//var client http.Client
	req, err := http.NewRequest("GET", w.Url, nil)
	// req.Header.Set("cookie", "")
	//log.Printf("Request header: %s\n", req.Header)
	if err != nil {
		return nil, 0, err
	}

	// Set range header
	req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	resp, err := PipelineHttp1.GetRawClient4Http2().Do(req) // myClient
	if err != nil {
		return nil, 0, err
	}
	size, err := strconv.ParseInt(resp.Header["Content-Length"][0], 10, 64)
	return resp.Body, size, err
}

func getHds(header http.Header, a ...string) string {
	for _, x := range a {
		if s, ok := header[x]; ok {
			if 0 < len(s) {
				return strings.Join(s, ",")
			}
		}
	}
	return ""
}

/*
1. Check if the URL supports Accept Ranges
2. Confirm the size of downloaded resources
*/
func getSizeAndCheckRangeSupport(szUrl1 string) (szFileName, szUrl string, size int64, err error) {
	req, err := http.NewRequest("HEAD", szUrl1, nil)
	if err != nil {
		return
	}
	// req.Header.Set("cookie", "")
	// log.Printf("Request header: %s\n", req.Header)
	res, err := PipelineHttp1.GetRawClient4Http2().Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()
	header := res.Header
	if s2 := getHds(header, "Location"); "" != s2 {
		return getSizeAndCheckRangeSupport(s2)
	}

	acceptRanges, supported := header["Accept-Ranges"]
	if !supported {
		return "", szUrl1, 0, errors.New("Doesn't support header `Accept-Ranges`.")
	} else if supported && acceptRanges[0] != "bytes" {
		return "", szUrl1, 0, errors.New("Support `Accept-Ranges`, but value is not `bytes`.")
	}
	if s1 := getHds(header, "Content-Length", "X-Linked-Size"); "" != s1 {
		size, err = strconv.ParseInt(s1, 10, 64)
	}
	szUrl = szUrl1
	// attachment; filename*=UTF-8''t2iadapter_style_sd14v1.pth; filename="t2iadapter_style_sd14v1.pth";
	if s1 := getHds(header, "Content-Disposition"); "" != s1 {
		a := strings.Split(s1, "; ")
		k := "filename*=UTF-8''"
		for _, x := range a {
			if strings.HasPrefix(x, k) {
				szFileName, err = url.QueryUnescape(x[len(k):])
				break
			}
		}
	}
	return
}

func getFileName(downloadUrl string) string {
	urlStruct, err := url.Parse(downloadUrl)
	handleError(err)
	return filepath.Base(urlStruct.Path)
}

func handleError(err error) {
	if err != nil {
		log.Println("err:", err)
		os.Exit(1)
	}
}
