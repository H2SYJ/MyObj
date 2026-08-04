package service

import (
	"myobj/src/pkg/models"

	"gorm.io/gorm"
)

func deleteUserFileTagRecords(tx *gorm.DB, userID, ufID string) error {
	if err := tx.Where("user_id = ? AND uf_id = ?", userID, ufID).Delete(&models.TagRebuildFailure{}).Error; err != nil {
		return err
	}
	if err := tx.Where("user_id = ? AND uf_id = ?", userID, ufID).Delete(&models.UserFileTagExclusion{}).Error; err != nil {
		return err
	}
	if err := tx.Where("user_id = ? AND uf_id = ?", userID, ufID).Delete(&models.UserFileTag{}).Error; err != nil {
		return err
	}
	return tx.Where("user_id = ? AND uf_id = ?", userID, ufID).Delete(&models.UserFileTagState{}).Error
}

func deleteFileMetadataRecords(tx *gorm.DB, fileID string) error {
	if err := tx.Where("file_id = ?", fileID).Delete(&models.FileMetadata{}).Error; err != nil {
		return err
	}
	return tx.Where("file_id = ?", fileID).Delete(&models.FileMetadataState{}).Error
}
