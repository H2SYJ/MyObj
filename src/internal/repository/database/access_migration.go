package database

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"myobj/src/pkg/custom_type"
	"myobj/src/pkg/models"
)

const defaultAccessSeedVersion = "20260814_default_access_seed"

// migrateDefaultAccessData 统一补齐空库和历史初始化脚本所需的默认组、权限及授权关系。
func migrateDefaultAccessData(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var applied int64
		if err := tx.Model(&schemaMigration{}).Where("version = ?", defaultAccessSeedVersion).Count(&applied).Error; err != nil {
			return fmt.Errorf("查询默认权限初始化状态失败: %w", err)
		}
		if applied > 0 {
			return nil
		}

		var adminGroup models.Group
		err := tx.Where("id = ?", models.DefaultAdminGroupID).First(&adminGroup).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			adminGroup = models.Group{
				ID: models.DefaultAdminGroupID, Name: "管理员", GroupDefault: 0,
				Space: 0, CreatedAt: custom_type.Now(),
			}
			if err := tx.Create(&adminGroup).Error; err != nil {
				return fmt.Errorf("创建管理员组失败: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("查询管理员组失败: %w", err)
		}

		defaultGroup, err := findOrCreateDefaultUserGroup(tx)
		if err != nil {
			return err
		}

		var existingPowers []*models.Power
		if err := tx.Find(&existingPowers).Error; err != nil {
			return fmt.Errorf("查询默认权限失败: %w", err)
		}
		powerMap := make(map[string]*models.Power, len(existingPowers))
		maxPowerID := 0
		for _, power := range existingPowers {
			powerMap[power.Characteristic] = power
			if power.ID > maxPowerID {
				maxPowerID = power.ID
			}
		}

		grants := make([]models.GroupPower, 0, len(models.DefaultPowerDefinitions)*2)
		for _, definition := range models.DefaultPowerDefinitions {
			power := powerMap[definition.Characteristic]
			if power == nil {
				maxPowerID++
				power = &models.Power{
					ID: maxPowerID, Name: definition.Name, Description: definition.Description,
					Characteristic: definition.Characteristic, CreatedAt: custom_type.Now(),
				}
				if err := tx.Create(power).Error; err != nil {
					return fmt.Errorf("创建默认权限%s失败: %w", definition.Characteristic, err)
				}
				powerMap[definition.Characteristic] = power
			}
			grants = append(grants, models.GroupPower{GroupID: adminGroup.ID, PowerID: power.ID})
			if definition.GrantToDefaultUser {
				grants = append(grants, models.GroupPower{GroupID: defaultGroup.ID, PowerID: power.ID})
			}
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&grants).Error; err != nil {
			return fmt.Errorf("写入默认组权限失败: %w", err)
		}
		if err := tx.Create(&schemaMigration{Version: defaultAccessSeedVersion, AppliedAt: time.Now()}).Error; err != nil {
			return fmt.Errorf("记录默认权限初始化状态失败: %w", err)
		}
		return nil
	})
}

func findOrCreateDefaultUserGroup(tx *gorm.DB) (*models.Group, error) {
	var defaultGroup models.Group
	err := tx.Where("group_default = ?", 1).Order("id ASC").First(&defaultGroup).Error
	if err == nil {
		// 旧初始化脚本中的 500 表示 500GB，但历史上没有换算成字节。
		if defaultGroup.Space == 500 {
			if err := tx.Model(&models.Group{}).Where("id = ?", defaultGroup.ID).
				Update("space", models.DefaultUserSpaceBytes).Error; err != nil {
				return nil, fmt.Errorf("修正默认用户组空间失败: %w", err)
			}
			defaultGroup.Space = models.DefaultUserSpaceBytes
		}
		return &defaultGroup, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("查询默认用户组失败: %w", err)
	}

	defaultGroupID := models.DefaultUserGroupID
	var occupied int64
	if err := tx.Model(&models.Group{}).Where("id = ?", defaultGroupID).Count(&occupied).Error; err != nil {
		return nil, fmt.Errorf("查询默认用户组ID失败: %w", err)
	}
	if occupied > 0 {
		if err := tx.Model(&models.Group{}).Select("COALESCE(MAX(id), 0) + 1").Scan(&defaultGroupID).Error; err != nil {
			return nil, fmt.Errorf("生成默认用户组ID失败: %w", err)
		}
	}
	defaultGroup = models.Group{
		ID: defaultGroupID, Name: "用户", GroupDefault: 1,
		Space: models.DefaultUserSpaceBytes, CreatedAt: custom_type.Now(),
	}
	if err := tx.Create(&defaultGroup).Error; err != nil {
		return nil, fmt.Errorf("创建默认用户组失败: %w", err)
	}
	return &defaultGroup, nil
}
