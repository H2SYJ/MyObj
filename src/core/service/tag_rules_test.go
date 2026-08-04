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

func TestSavePersonalDictionaryCreatesRulesOnlyOnce(t *testing.T) {
	service, db := newTagFailureTestService(t,
		&models.TagCategory{}, &models.TagRuleSet{}, &models.TagRule{},
		&models.TagRebuildJob{}, &tagFailureTestUserFile{},
	)
	now := time.Now()
	if err := db.Create(&models.TagCategory{
		ID: "title", Code: "title", Name: "标题", Color: "#409eff",
		Enabled: true, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	global := &models.TagRuleSet{
		ID: "global-v1", ScopeType: models.TagRuleScopeGlobal, Version: 1,
		Status: models.TagRuleSetActive,
	}
	service.globalRuntime.Store(&globalTagRuntime{ruleSet: global})
	service.autoLimit.Store(20)

	inputs := []request.TagRuleInput{{
		Type: models.TagRuleTypeWord, Pattern: "人工智能",
		CategoryID: "title", Weight: 1, Enabled: true,
	}}
	first, _, err := service.SavePersonalDictionary(context.Background(), "user-1", inputs)
	if err != nil {
		t.Fatalf("首次保存个人词典失败: %v", err)
	}
	if len(first.Rules) != 1 {
		t.Fatalf("首次保存的规则数量不正确: %+v", first.Rules)
	}

	// 模拟前端读取后原样回传旧版本规则 ID；新版本必须生成新主键且不能重复插入。
	inputs[0].ID = first.Rules[0].ID
	second, _, err := service.SavePersonalDictionary(context.Background(), "user-1", inputs)
	if err != nil {
		t.Fatalf("再次保存个人词典失败: %v", err)
	}
	if len(second.Rules) != 1 || second.Rules[0].ID == first.Rules[0].ID {
		t.Fatalf("个人词典新版本没有生成独立规则主键: first=%+v second=%+v", first.Rules, second.Rules)
	}

	var ruleCount int64
	if err := db.Model(&models.TagRule{}).Count(&ruleCount).Error; err != nil {
		t.Fatal(err)
	}
	if ruleCount != 2 {
		t.Fatalf("两次保存应各写入一条规则，实际为 %d", ruleCount)
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
