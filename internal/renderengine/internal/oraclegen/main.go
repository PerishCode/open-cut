package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

const knotCount = 4097

func main() {
	var output bytes.Buffer
	for _, inverse := range []bool{true, false} {
		for index := 0; index < knotCount; index++ {
			code := index * 16
			if index == knotCount-1 {
				code = 65535
			}
			value := float64(code) / 65535
			if inverse {
				value = inverseRec709(value)
			} else {
				value = forwardRec709(value)
			}
			quantized := math.RoundToEven(value * 65535)
			if quantized < 0 || quantized > 65535 {
				panic("Rec.709 oracle knot is outside uint16")
			}
			if err := binary.Write(&output, binary.BigEndian, uint16(quantized)); err != nil {
				panic(err)
			}
		}
	}
	root, err := repositoryRoot()
	if err != nil {
		panic(err)
	}
	filename := filepath.Join(root, "internal", "renderengine", "rec709_lut.bin")
	if err := os.WriteFile(filename, output.Bytes(), 0o600); err != nil {
		panic(err)
	}
	digest := sha256.Sum256(output.Bytes())
	fmt.Printf("%x\n", digest)
}

func inverseRec709(value float64) float64 {
	if value < 0.081 {
		return value / 4.5
	}
	return math.Pow((value+0.099)/1.099, 1/0.45)
}

func forwardRec709(value float64) float64 {
	if value < 0.018 {
		return 4.5 * value
	}
	return 1.099*math.Pow(value, 0.45) - 0.099
}

func repositoryRoot() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("repository root not found")
		}
		current = parent
	}
}
