package models

import "time"

// 演出
type Show struct {
	ID              uint   `json:"id" gorm:"primaryKey"`
	Name            string `json:"name"`
	PerformanceDate string `json:"performanceDate"`
	// 不同换装点之间的转场时间（秒）
	TransferSeconds int64 `json:"transferSeconds"`
	// 不同角色/演员共用同一件戏服时要求的最短清洁间隔（秒）
	CleaningSeconds int64  `json:"cleaningSeconds"`
	Status          string `json:"status"` // planned / ongoing / closed
	CreatedAt       time.Time
}

// 角色
type Role struct {
	ID     uint   `json:"id" gorm:"primaryKey"`
	ShowID uint   `json:"showId"`
	Name   string `json:"name"`
}

// 演员
type Actor struct {
	ID   uint   `json:"id" gorm:"primaryKey"`
	Name string `json:"name"`
}

// 场次（幕/场）
type Scene struct {
	ID     uint   `json:"id" gorm:"primaryKey"`
	ShowID uint   `json:"showId"`
	Name   string `json:"name"`
	Seq    int    `json:"seq"`
}

// 戏服（每件实物，唯一编号；没有“库存数量”概念）
type Costume struct {
	ID            uint   `json:"id" gorm:"primaryKey"`
	Code          string `json:"code" gorm:"uniqueIndex"` // 实物编号/条码
	Name          string `json:"name"`
	ChangeSeconds int64  `json:"changeSeconds"` // 预计换装时间（秒）
	// available 可用 / in_use 使用中 / laundering 洗护中 /
	// repairing 维修中 / missing 少件 / damaged 破损
	Status   string `json:"status"`
	Location string `json:"location"` // 当前所在位置
	HolderID *uint  `json:"holderId"` // 当前持有人（签收演员）
}

// 换装交接任务：演员上场顺序上的一个换装点
type Task struct {
	ID        uint `json:"id" gorm:"primaryKey"`
	ShowID    uint `json:"showId"`
	CostumeID uint `json:"costumeId"`
	RoleID    uint `json:"roleId"`
	ActorID   uint `json:"actorId"`  // 当前计划演员（临时换角只改这里）
	SceneID   uint `json:"sceneId"`
	Seq       int  `json:"seq"` // 上场顺序

	OnAt        time.Time `json:"onAt"`        // 上场时刻（须已穿好）
	OffAt       time.Time `json:"offAt"`       // 下场时刻（衣服可交回）
	ChangePoint string    `json:"changePoint"` // 换装点，如 侧台左/侧台右/后台

	// 排程时的原演员；临时换角后仍保留，已签收记录通过它关联原演员
	OriginalActorID uint `json:"originalActorId"`

	// pending 待签收 / signed 已签收 / returned 已交回 / cancelled 已取消
	Status string `json:"status"`

	SignedByActorID *uint     `json:"signedByActorId"` // 实际签收演员（签收后冻结）
	SignedAt        *time.Time `json:"signedAt"`
	ReturnedAt      *time.Time `json:"returnedAt"`

	// 强制保存时记录的冲突说明（场次页面可见）
	ConflictNote string `json:"conflictNote"`
	Note         string `json:"note"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// 洗护 / 维修记录
type CareRecord struct {
	ID        uint       `json:"id" gorm:"primaryKey"`
	CostumeID uint       `json:"costumeId"`
	// laundry 洗护 / repair 维修
	Type      string     `json:"type"`
	Status    string     `json:"status"` // open / done
	Note      string     `json:"note"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt"`
}

// 少件 / 破损问题单：必须走处理流程，不能靠改数量消除
type Issue struct {
	ID         uint       `json:"id" gorm:"primaryKey"`
	CostumeID  uint       `json:"costumeId"`
	ShowID     *uint      `json:"showId"`
	// missing 少件 / damaged 破损
	Type       string     `json:"type"`
	Status     string     `json:"status"` // open / resolved
	Note       string     `json:"note"`
	Resolution string     `json:"resolution"`
	ReportedAt time.Time  `json:"reportedAt"`
	ResolvedAt *time.Time `json:"resolvedAt"`
}

// 散场清点
type CheckSession struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	ShowID    uint      `json:"showId"`
	CreatedAt time.Time `json:"createdAt"`
	Closed    bool      `json:"closed"`
}

type CheckItem struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	SessionID uint      `json:"sessionId"`
	CostumeID uint      `json:"costumeId"`
	// present 在场 / missing 少件 / damaged 破损
	State     string    `json:"state"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"createdAt"`
}

// 状态常量
const (
	CostumeAvailable  = "available"
	CostumeInUse      = "in_use"
	CostumeLaundering = "laundering"
	CostumeRepairing  = "repairing"
	CostumeMissing    = "missing"
	CostumeDamaged    = "damaged"

	TaskPending   = "pending"
	TaskSigned    = "signed"
	TaskReturned  = "returned"
	TaskCancelled = "cancelled"

	CareLaundry = "laundry"
	CareRepair  = "repair"
	CareOpen    = "open"
	CareDone    = "done"

	IssueMissing  = "missing"
	IssueDamaged  = "damaged"
	IssueOpen     = "open"
	IssueResolved = "resolved"

	CheckPresent = "present"
	CheckMissing = "missing"
	CheckDamaged = "damaged"
)

func AllModels() []any {
	return []any{
		&Show{}, &Role{}, &Actor{}, &Scene{}, &Costume{},
		&Task{}, &CareRecord{}, &Issue{}, &CheckSession{}, &CheckItem{},
	}
}
