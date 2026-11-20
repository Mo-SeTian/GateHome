package database

import (
	"errors"
	"log"
	"os"
	"path"
	"sun-panel/lib/cmn"
	"sun-panel/models"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	_ "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

const (
	MYSQL  = "mysql"
	SQLITE = "sqlite"
)

type DbClient interface {
	Connect() (db *gorm.DB, err error)
}

type MySQLConfig struct {
	Username    string
	Password    string
	Host        string
	Port        string
	Database    string
	WaitTimeout int
}

type SQLiteConfig struct {
	Filename string
}

func DbInit(dbClient DbClient) (db *gorm.DB, dbErr error) {
	db, dbErr = dbClient.Connect()
	if dbErr != nil {
		return
	}
	return
}

// Connect mysql连接
func (d *MySQLConfig) Connect() (db *gorm.DB, err error) {
	dsn := d.Username + ":" + d.Password + "@tcp(" + d.Host + ":" + d.Port + ")/" + d.Database + "?charset=utf8mb4&parseTime=True&loc=Local"
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: GetLogger(),
		NamingStrategy: schema.NamingStrategy{
			// TablePrefix:   "blog_",
			SingularTable: true,
		},
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	sqlDb, _ := db.DB()
	sqlDb.SetMaxIdleConns(10)  // SetMaxIdleConns 设置空闲连接池中连接的最大数量
	sqlDb.SetMaxOpenConns(100) // SetMaxOpenConns 设置打开数据库连接的最大数量。
	wait_timeout := d.WaitTimeout
	sqlDb.SetConnMaxLifetime(time.Duration(wait_timeout * int(time.Second))) // SetConnMaxLifetime 设置了连接可复用的最大时间。
	return
}

// Connect sqllite3连接
func (d *SQLiteConfig) Connect() (db *gorm.DB, err error) {
	filePath := d.Filename
	exists := false
	if exists, err = cmn.PathExists(path.Dir(filePath)); err != nil {
		return
	} else {

		// 创建文件夹
		if !exists {
			if err = os.MkdirAll(path.Dir(filePath), 0700); err != nil {
				return
			}
		}

		db, err = gorm.Open(sqlite.Open(filePath), &gorm.Config{
			Logger: GetLogger(),
			NamingStrategy: schema.NamingStrategy{
				SingularTable: true,
			},
		})
	}

	return
}

// 日志
func GetLogger() logger.Interface {
	return logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer（日志输出的目标，前缀和日志包含的内容——译者注）
		logger.Config{
			SlowThreshold:             time.Second,   // 慢 SQL 阈值
			LogLevel:                  logger.Silent, // 日志级别
			IgnoreRecordNotFoundError: true,          // 忽略ErrRecordNotFound（记录未找到）错误
			Colorful:                  true,          // 彩色打印
		},
	)

}

// 创建数据库
func CreateDatabase(driver string, db *gorm.DB) error {

	// mysql特殊处理
	if driver == MYSQL {
		db = db.Set("gorm:table_options", "ENGINE=InnoDB")
	}

	// 创建数据表
	err := db.AutoMigrate(
		&models.User{},
		&models.SystemSetting{},
		&models.ItemIcon{},
		&models.UserConfig{},
		&models.File{},
		&models.ItemIconGroup{},
		&models.ModuleConfig{},
	)

	return err
}

// A pending account has no valid password hash. Only the trusted operator can finish setup.
const PendingPassword = "!setup-required"

func NotFoundAndCreateUser(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var first models.User
		err := tx.Select("id").First(&first).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			const username = "admin@sun.cc"
			return tx.Create(&models.User{Username: username, Mail: username, Name: username, Role: 1, Status: 1, Password: PendingPassword}).Error
		}
		if err != nil {
			return err
		}
		var admins []models.User
		if err := tx.Select("id", "password").Where("role = ?", 1).Find(&admins).Error; err != nil {
			return err
		}
		for _, admin := range admins {
			if cmn.UsesFactoryPassword(admin.Password) {
				if err := tx.Model(&admin).Where("password = ?", admin.Password).Updates(map[string]any{"password": PendingPassword, "token": ""}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}
