package service

import (
	"context"
	"testing"
	"time"

	"myobj/src/core/domain/request"
	"myobj/src/pkg/models"
)

func TestBuildPersonalRulesRegeneratesPrimaryKeys(t *testing.T) {
	inputs := []request.TagRuleInput{{
		ID: "archived-rule-id", Type: "word", Pattern: "人工智能",
		CategoryID: "title", Weight: 1, Enabled: true,
	}}
	personal, err := buildRules(inputs, "personal-v2", true)
	if err != nil {
		t.Fatal(err)
	}
	if personal[0].ID == "archived-rule-id" || personal[0].RuleSetID != "personal-v2" {
		t.Fatalf("个人词典新版本复用了旧规则主键: %+v", personal[0])
	}

	global, err := buildRules(inputs, "global-draft", false)
	if err != nil {
		t.Fatal(err)
	}
	if global[0].ID != "archived-rule-id" {
		t.Fatalf("全局草稿保存应保留同一草稿内的规则ID: %+v", global[0])
	}
}

func TestRuleCategoryValidationAndDeleteProtection(t *testing.T) {
	service, db := newTagFailureTestService(t, &models.TagCategory{}, &models.TagDefinition{}, &models.TagRule{})
	now := time.Now()
	category := models.TagCategory{
		ID: "custom", Code: "custom", Name: "自定义", Color: "#409eff",
		Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	inputs := []request.TagRuleInput{{Type: models.TagRuleTypeWord, Pattern: "人工智能", CategoryID: category.ID, Enabled: true}}
	if err := service.validateRuleCategories(context.Background(), inputs); err != nil {
		t.Fatalf("存在的规则分类被拒绝: %v", err)
	}
	inputs[0].CategoryID = "missing"
	if err := service.validateRuleCategories(context.Background(), inputs); err == nil {
		t.Fatal("不存在的规则分类应被拒绝")
	}
	rule := models.TagRule{
		ID: "rule-1", RuleSetID: "draft-1", Type: models.TagRuleTypeWord,
		TargetField: "basename", Pattern: "人工智能", CategoryID: category.ID,
		Weight: 1, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteCategory(context.Background(), category.ID); err == nil {
		t.Fatal("规则仍引用分类时不应允许删除")
	}
}
