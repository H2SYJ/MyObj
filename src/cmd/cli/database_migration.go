package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"myobj/src/internal/repository/database"
)

const migrationMySQLDSNEnv = "MYOBJ_MIGRATE_MYSQL_DSN"

func databaseCommand() *cli.Command {
	return &cli.Command{
		Name:  "database",
		Usage: "数据库迁移与校验",
		Subcommands: []*cli.Command{
			{
				Name:   "migrate-sqlite-to-mysql",
				Usage:  "将SQLite一致性快照迁移到空MySQL数据库",
				Action: migrateSQLiteToMySQLAction,
				Flags:  sqliteToMySQLFlags(true),
			},
			{
				Name:   "verify-sqlite-to-mysql",
				Usage:  "只读校验SQLite快照与已迁移MySQL",
				Action: verifySQLiteToMySQLAction,
				Flags:  sqliteToMySQLFlags(false),
			},
		},
	}
}

func sqliteToMySQLFlags(includeMigrationFlags bool) []cli.Flag {
	flags := []cli.Flag{
		&cli.StringFlag{Name: "source", Usage: "源SQLite文件或迁移快照路径", Required: true},
		&cli.IntFlag{Name: "batch-size", Usage: "每批提交行数（100-10000）", Value: 1000},
		&cli.StringFlag{Name: "timezone", Usage: "无时区时间的解释时区", Value: "Asia/Shanghai"},
	}
	if includeMigrationFlags {
		flags = append(flags,
			&cli.StringFlag{Name: "snapshot", Usage: "一致性快照输出路径，默认生成在源文件旁"},
			&cli.BoolFlag{Name: "dry-run", Usage: "生成并升级快照、检查目标空库，但不写MySQL"},
			&cli.BoolFlag{Name: "yes", Aliases: []string{"y"}, Usage: "跳过交互式确认"},
		)
	}
	return flags
}

func migrateSQLiteToMySQLAction(c *cli.Context) error {
	dsn, err := migrationMySQLDSN()
	if err != nil {
		return err
	}
	options := database.SQLiteToMySQLOptions{
		SourcePath:   c.String("source"),
		SnapshotPath: c.String("snapshot"),
		MySQLDSN:     dsn,
		BatchSize:    c.Int("batch-size"),
		Timezone:     c.String("timezone"),
		DryRun:       c.Bool("dry-run"),
		Progress:     printMigrationProgress,
	}

	pterm.DefaultHeader.WithFullWidth().Println("SQLite → MySQL 数据迁移")
	pterm.Info.Printf("源SQLite: %s\n", options.SourcePath)
	if options.SnapshotPath != "" {
		pterm.Info.Printf("快照路径: %s\n", options.SnapshotPath)
	} else {
		pterm.Info.Println("快照路径: 自动生成在源文件旁")
	}
	pterm.Info.Printf("目标MySQL: %s\n", database.RedactMySQLDSN(dsn))
	if options.DryRun {
		pterm.Warning.Println("当前为dry-run：不会创建MySQL表或写入数据，但会保留SQLite快照")
	} else if !c.Bool("yes") {
		confirmed := false
		prompt := &survey.Confirm{
			Message: "确认MyObj、WebDAV和所有后台任务均已停止，且目标MySQL是可重建的空库？",
			Default: false,
		}
		if err := survey.AskOne(prompt, &confirmed); err != nil {
			return err
		}
		if !confirmed {
			return errorsNewMigrationCancelled()
		}
	}

	report, err := database.MigrateSQLiteToMySQL(c.Context, options)
	if err != nil {
		return err
	}
	printMigrationReport(report)
	if report.DryRun {
		pterm.Success.Println("dry-run完成，MySQL未写入")
	} else {
		pterm.Success.Println("SQLite → MySQL迁移及校验完成")
	}
	return nil
}

func verifySQLiteToMySQLAction(c *cli.Context) error {
	dsn, err := migrationMySQLDSN()
	if err != nil {
		return err
	}
	pterm.DefaultHeader.WithFullWidth().Println("SQLite → MySQL 迁移校验")
	pterm.Info.Printf("SQLite快照: %s\n", c.String("source"))
	pterm.Info.Printf("目标MySQL: %s\n", database.RedactMySQLDSN(dsn))
	report, err := database.VerifySQLiteToMySQL(c.Context, database.SQLiteToMySQLOptions{
		SourcePath: c.String("source"),
		MySQLDSN:   dsn,
		BatchSize:  c.Int("batch-size"),
		Timezone:   c.String("timezone"),
	})
	if err != nil {
		return err
	}
	printMigrationReport(report)
	pterm.Success.Println("SQLite与MySQL数据校验通过")
	return nil
}

func migrationMySQLDSN() (string, error) {
	dsn := strings.TrimSpace(os.Getenv(migrationMySQLDSNEnv))
	if dsn == "" {
		return "", fmt.Errorf("请通过环境变量%s提供目标MySQL DSN", migrationMySQLDSNEnv)
	}
	return dsn, nil
}

func printMigrationProgress(progress database.MigrationProgress) {
	switch {
	case progress.Stage == "preflight" && progress.Table != "" && progress.Total > 0:
		pterm.Info.Printf("预检 %-28s %d/%d\n", progress.Table, progress.Completed, progress.Total)
	case progress.Stage == "copy" && progress.Table != "" && progress.Total > 0:
		pterm.Info.Printf("迁移 %-28s %d/%d\n", progress.Table, progress.Completed, progress.Total)
	case progress.Message != "":
		pterm.Info.Println(progress.Message)
	}
}

func printMigrationReport(report *database.SQLiteToMySQLReport) {
	data := pterm.TableData{{"表", "SQLite行数", "MySQL行数", "摘要"}}
	var sourceTotal, targetTotal int64
	for _, table := range report.Tables {
		digestStatus := "待迁移"
		if table.SourceDigest != "" && table.SourceDigest == table.TargetDigest {
			digestStatus = "一致"
		} else if table.SourceDigest != "" {
			digestStatus = "不一致"
		}
		data = append(data, []string{
			table.Table,
			fmt.Sprintf("%d", table.SourceRows),
			fmt.Sprintf("%d", table.TargetRows),
			digestStatus,
		})
		sourceTotal += table.SourceRows
		targetTotal += table.TargetRows
	}
	pterm.DefaultTable.WithHasHeader().WithData(data).Render()
	pterm.Info.Printf("SQLite总行数: %d，MySQL总行数: %d\n", sourceTotal, targetTotal)
	pterm.Info.Printf("保留快照: %s\n", report.SnapshotPath)
}

func errorsNewMigrationCancelled() error {
	return fmt.Errorf("用户取消迁移")
}
