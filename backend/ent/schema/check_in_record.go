package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
)

// CheckInRecord holds the schema definition for the CheckInRecord entity.
//
// 删除策略：硬删除
// CheckInRecord 使用硬删除而非软删除，原因如下：
//   - 签到记录本质上是发钱审计流水，只追加不修改
//   - 硬删除 + 普通唯一索引即可保证"每用户每天最多一条"，无需部分唯一索引
//   - 用户被软删除时签到记录保留，方便管理员审计
type CheckInRecord struct {
	ent.Schema
}

func (CheckInRecord) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "check_in_records"},
	}
}

func (CheckInRecord) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (CheckInRecord) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		// 自然日零点（应用全局时区），格式语义上等同 YYYY-MM-DD
		field.Time("check_in_date").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		// 本次签到时的连续天数（1 起），用于展示与审计，配置修改不影响历史
		field.Int("streak_days").
			Default(1),
		// 实发金额（USD），审计时以此为准
		field.Float("reward_amount").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Default(0),
	}
}

func (CheckInRecord) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("check_in_records").
			Field("user_id").
			Unique().
			Required(),
	}
}

func (CheckInRecord) Indexes() []ent.Index {
	return []ent.Index{
		// 幂等兜底：每用户每天最多一条签到记录（真相来源是数据库）。
		// 该唯一索引已覆盖月封顶统计的 (user_id, check_in_date) 前缀查询，无需重复索引。
		index.Fields("user_id", "check_in_date").
			Unique(),
		index.Fields("user_id"),
	}
}
