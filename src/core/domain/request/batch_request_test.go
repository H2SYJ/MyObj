package request

import "testing"

func TestBatchRequestsValidateUniqueLimit(t *testing.T) {
	shareIDs := make([]int, 0, 202)
	for id := 1; id <= 200; id++ {
		shareIDs = append(shareIDs, id)
	}
	shareIDs = append(shareIDs, 1, 2)
	if err := (&BatchDeleteShareRequest{IDs: shareIDs}).ValidateUniqueLimit(200); err != nil {
		t.Fatalf("重复分享 ID 不应计入批量上限: %v", err)
	}
	shareIDs = append(shareIDs, 201)
	if err := (&BatchDeleteShareRequest{IDs: shareIDs}).ValidateUniqueLimit(200); err == nil {
		t.Fatal("超过 200 个去重分享 ID 应被拒绝")
	}

	recycledIDs := make([]string, 0, 202)
	for id := 0; id < 200; id++ {
		recycledIDs = append(recycledIDs, string(rune(id+1)))
	}
	recycledIDs = append(recycledIDs, recycledIDs[0], recycledIDs[1])
	if err := (&BatchRecycledRequest{RecycledIDs: recycledIDs}).ValidateUniqueLimit(200); err != nil {
		t.Fatalf("重复回收站 ID 不应计入批量上限: %v", err)
	}
	recycledIDs = append(recycledIDs, "第201项")
	if err := (&BatchRecycledRequest{RecycledIDs: recycledIDs}).ValidateUniqueLimit(200); err == nil {
		t.Fatal("超过 200 个去重回收站 ID 应被拒绝")
	}
}
