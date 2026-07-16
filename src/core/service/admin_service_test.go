package service

import (
	"math"
	"myobj/src/pkg/util"
	"testing"
)

func TestDiskSizeGBToBytes(t *testing.T) {
	size, err := diskSizeGBToBytes(447)
	if err != nil {
		t.Fatalf("转换磁盘容量失败: %v", err)
	}
	if size != 447*util.DiskByte {
		t.Fatalf("磁盘容量转换错误: got=%d want=%d", size, 447*util.DiskByte)
	}
}

func TestDiskSizeGBToBytesRejectsInvalidValue(t *testing.T) {
	if _, err := diskSizeGBToBytes(0); err == nil {
		t.Fatal("0GB应返回错误")
	}
	if _, err := diskSizeGBToBytes(math.MaxInt64); err == nil {
		t.Fatal("溢出的磁盘容量应返回错误")
	}
}
