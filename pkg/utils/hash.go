package utils

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

func CalculateFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %v", err)
	}
	defer file.Close()

	hasher := md5.New()
	_, err = io.Copy(hasher, file)
	if err != nil {
		return "", fmt.Errorf("failed to hash file: %v", err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func CompareFileHash(filePath string, expectedHash string) (bool, error) {
	hash, err := CalculateFileHash(filePath)
	if err != nil {
		return false, err
	}
	return hash == expectedHash, nil
}
