package gobls_test

import (
	"github.com/rlj1202/go-BitmapLogicSimulator"
	"image"
	_ "image/png"
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	imgFile, err := os.Open("test.png")
	if err != nil {
		t.Error(err)
		return
	}

	img, _, err := image.Decode(imgFile)
	if err != nil {
		panic(err)
	}

	simulator := gobls.NewSimulator()
	simulator.LoadImage(img)
	simulator.Simulate()
}
