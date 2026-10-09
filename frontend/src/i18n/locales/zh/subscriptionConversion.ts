export default {
  title: '订阅转余额', adminIntro: '允许用户将自主购买的有效订阅折算到账户余额。', userIntro: '查看有效订阅的可转换金额，确认后余额到账，原订阅撤销。',
  enabled: '向用户开放订阅转余额', enabledHint: '开启后，用户左侧菜单显示此页面，同时允许提交转换。关闭后入口隐藏，转换接口也会关闭。', save: '保存配置', saved: '配置已保存', saving: '保存中…', refresh: '刷新', loading: '正在加载…', retry: '重新加载',
  formulaTitle: '转换规则', formula: '转换余额 = 购买售价 ×（剩余时间比例 × 50% + 剩余额度比例 × 50%）',
  timeRule: '剩余时间比例 = 剩余秒数 ÷ 购买总秒数。', quotaRule: '总额度 = 月额度 × 购买天数 ÷ 30；剩余额度比例 =（总额度 − 累计用量）÷ 总额度，最低为 0。',
  priceRule: '售价取已完成支付订单保存的购买金额，续费按本次有效订阅关联的订单累计；不使用套餐当前标价，也不包含支付手续费。',
  exampleTitle: '计算示例', example: '售价 $30，剩余时间 50%，剩余额度 80%：$30 ×（50% × 50% + 80% × 50%）= $19.50。',
  precision: '按确认时的有效时间和已记账用量结算，金额向下保留 6 位小数。余额单位沿用账户计价单位 USD。',
  eligibility: '哪些订阅可以转换', eligibleHint: '仅支持可核实支付来源、按月额度计费的有效订阅。管理员分配、兑换码赠送、混合赠送天数及退款中的订单不可自助转换。',
  historyHint: '历史用量被清理、无法核实来源或期限的订阅会显示原因，不会按零用量折算。转换后不能恢复原订阅，关联订单也不再支持退款。',
  closed: '订阅转余额暂未开放', closedHint: '管理员开放后，你可以在此查看转换金额。', activeTitle: '当前有效订阅', empty: '暂无有效订阅', emptyHint: '自主购买的有效订阅将在这里显示。',
  group: '订阅', expiry: '到期时间', price: '购买售价', remainingTime: '剩余时间', remainingQuota: '剩余额度', monthlyQuota: '月额度', totalQuota: '订阅总额度', usedQuota: '累计用量', estimate: '预计到账', days: '{days} 天', orders: '购买订单',
  convert: '转换为余额', unavailable: '不可转换', details: '计算明细', timeRatio: '时间剩余比例', quotaRatio: '额度剩余比例',
  confirmTitle: '确认转换此订阅', confirmHint: '转换成功后将立即撤销「{name}」。绑定该订阅分组的密钥将失去此订阅权益，后续调用按分组规则处理。', acknowledge: '我已了解：转换后无法恢复原订阅，关联订单不能再退款。', confirm: '确认转换', cancel: '取消', converting: '转换中…', success: '转换成功，已到账 ${amount}', stale: '页面中的金额是预估值，最终以确认时计算的金额为准。',
  history: '转换记录', historyLimit: '展示最近 100 条，完整记录保存在后台。', historyEmpty: '还没有转换记录', convertedAt: '转换时间', credited: '实际到账', converted: '已转余额',
  reasons: { not_purchased: '仅限自主购买；此订阅的支付来源无法确认', inactive: '订阅已失效或尚未开始', mixed_term: '期限包含非购买调整，无法自助折算', usage_incomplete: '历史用量不完整，无法准确折算', unsupported_quota: '仅支持设置月额度、未设置日／周额度的订阅', order_unavailable: '关联订单未完成、已退款或已申请退款', too_small: '可转换金额不足 0.000001 USD', pending_requests: '仍有请求或用量待结算，请稍后刷新重试', unsettled_usage: '有未成功结算的请求，请联系管理员核对' },
  failed: '操作失败，请稍后重试'
}
