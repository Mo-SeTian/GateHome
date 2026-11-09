<template>
  <div class="certificates-page animate-fade-in">
    <div class="page-topbar">
      <div>
        <h1 class="page-title">SSL / TLS 证书管理</h1>
        <p class="page-subtitle">Let's Encrypt 自动申请、ACME HTTP-01/DNS-01 验证与全生命周期续期追踪</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-secondary" @click="checkRenewAll">
          <RefreshCw :size="14" :class="{ 'spin-icon': isChecking }" />
          检查全部到期续签
        </button>
        <button class="btn btn-primary" @click="openApplyModal">
          <Plus :size="15" />
          申请新证书
        </button>
      </div>
    </div>

    <!-- Certificates Grid -->
    <div class="certs-grid">
      <SpotlightCard 
        v-for="cert in certificates" 
        :key="cert.id"
        class="cert-card"
      >
        <div class="card-head">
          <div class="lock-icon-box" :class="cert.status">
            <Lock :size="18" />
          </div>
          <div class="cert-status-group">
            <span class="badge" :class="cert.status === 'valid' ? 'badge-ok' : 'badge-warn'">
              <span class="status-dot" :class="cert.status === 'valid' ? 'ok' : 'warn'" />
              {{ cert.status === 'valid' ? '证书有效' : '即将到期' }}
            </span>
          </div>
        </div>

        <div class="cert-domain-title mono-text">{{ cert.domain }}</div>

        <div class="cert-meta-box">
          <div class="meta-item">
            <span class="meta-label">颁发者 (CA):</span>
            <span class="meta-val">{{ cert.issuer }}</span>
          </div>
          <div class="meta-item">
            <span class="meta-label">加密算法:</span>
            <span class="meta-val mono-text">{{ cert.keyType }}</span>
          </div>
          <div class="meta-item">
            <span class="meta-label">到期时间:</span>
            <span class="meta-val mono-text">{{ cert.expiresAt }}</span>
          </div>
          <div class="meta-item">
            <span class="meta-label">剩余天数:</span>
            <span class="days-pill" :class="{ warn: cert.daysLeft < 30 }">
              {{ cert.daysLeft }} 天
            </span>
          </div>
        </div>

        <div class="card-footer">
          <div class="auto-renew-chip">
            <CheckCircle :size="12" class="ok-icon" />
            <span>自动轮换续期已启用</span>
          </div>
          <div class="actions">
            <button class="btn btn-secondary btn-sm" @click="renewCert(cert)">
              立即续期
            </button>
            <button class="btn btn-ghost btn-sm text-bad" @click="deleteCert(cert)">
              <Trash2 :size="13" />
            </button>
          </div>
        </div>
      </SpotlightCard>
    </div>

    <!-- Apply Modal -->
    <transition name="modal-fade">
      <div v-if="isModalOpen" class="modal-backdrop" @click.self="isModalOpen = false">
        <div class="modal-dialog">
          <div class="modal-header">
            <h3 class="modal-title">申请 Let's Encrypt SSL 证书</h3>
            <button class="close-modal-btn" @click="isModalOpen = false"><X :size="18" /></button>
          </div>
          <form class="modal-body" @submit.prevent="applyNewCert">
            <div class="form-group">
              <label class="form-label">申请域名 (多个域名逗号分隔)</label>
              <input v-model="form.domain" type="text" placeholder="example.com, *.example.com" class="form-input mono-text" required />
            </div>
            <div class="form-group">
              <label class="form-label">验证方式 (Challenge)</label>
              <select v-model="form.challenge" class="form-select">
                <option value="http">HTTP-01 (自动放置 Token 文件)</option>
                <option value="dns">DNS-01 (自动配置 Cloudflare/Aliyun DNS TXT)</option>
              </select>
            </div>
            <div class="form-group">
              <label class="form-label">私钥算法</label>
              <select v-model="form.keyType" class="form-select">
                <option value="ECDSA P-256">ECDSA P-256 (推荐 · 更快更安全)</option>
                <option value="RSA 2048">RSA 2048 (兼容老设备)</option>
                <option value="RSA 4096">RSA 4096 (高强度)</option>
              </select>
            </div>
            <div class="modal-footer">
              <button type="button" class="btn btn-secondary" @click="isModalOpen = false">取消</button>
              <button type="submit" class="btn btn-primary" :disabled="isSubmitting">
                {{ isSubmitting ? '正在签发中...' : '提交签发' }}
              </button>
            </div>
          </form>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { RefreshCw, Plus, Lock, CheckCircle, Trash2, X } from 'lucide-vue-next'
import SpotlightCard from '@/components/SpotlightCard.vue'
import { useAppStore } from '@/composables/useAppStore'
import { useToast } from '@/composables/useToast'

const { certificates } = useAppStore()
const { success, info } = useToast()

const isChecking = ref(false)
const isModalOpen = ref(false)
const isSubmitting = ref(false)

const form = ref({
  domain: '',
  challenge: 'http',
  keyType: 'ECDSA P-256'
})

const checkRenewAll = () => {
  isChecking.value = true
  setTimeout(() => {
    isChecking.value = false
    success('所有证书有效性已复核完毕，下一次自动续签预定在到期前 30 天')
  }, 700)
}

const renewCert = (cert) => {
  cert.daysLeft = 90
  cert.status = 'valid'
  cert.expiresAt = '2027-02-09'
  success(`证书 "${cert.domain}" 续期成功，有效期已延长至 90 天`)
}

const openApplyModal = () => {
  form.value = {
    domain: '',
    challenge: 'http',
    keyType: 'ECDSA P-256'
  }
  isModalOpen.value = true
}

const applyNewCert = () => {
  isSubmitting.value = true
  setTimeout(() => {
    certificates.value.unshift({
      id: `cert_${Date.now()}`,
      domain: form.value.domain,
      issuer: "Let's Encrypt Authority X3",
      expiresAt: '2027-02-09',
      daysLeft: 90,
      status: 'valid',
      autoRenew: true,
      keyType: form.value.keyType
    })
    isSubmitting.value = false
    isModalOpen.value = false
    success(`SSL 证书 "${form.value.domain}" 已成功签发并部署`)
  }, 1000)
}

const deleteCert = (cert) => {
  if (confirm(`确定要移除证书 "${cert.domain}" 吗？关联的反代将无法使用 HTTPS`)) {
    certificates.value = certificates.value.filter(c => c.id !== cert.id)
    info(`已删除证书: ${cert.domain}`)
  }
}
</script>

<style scoped>
.certificates-page { display: flex; flex-direction: column; gap: 20px; }
.page-topbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 16px; }
.page-title { font-size: 22px; font-weight: 700; color: var(--ink); }
.page-subtitle { font-size: 13px; color: var(--muted); margin-top: 3px; }
.header-actions { display: flex; gap: 10px; }
.spin-icon { animation: spin 0.8s linear infinite; }
@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }

.certs-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 16px; }
.cert-card { padding: 18px; display: flex; flex-direction: column; gap: 12px; }

.card-head { display: flex; align-items: center; justify-content: space-between; }
.lock-icon-box {
  width: 34px; height: 34px; border-radius: var(--radius-sm);
  display: flex; align-items: center; justify-content: center;
  border: 1px solid var(--line);
}
.lock-icon-box.valid { background: rgba(16, 185, 129, 0.12); color: var(--ok); }
.lock-icon-box.expiring_soon { background: rgba(245, 158, 11, 0.12); color: var(--warn); }

.cert-domain-title { font-size: 14.5px; font-weight: 700; color: var(--ink); }

.cert-meta-box {
  background: var(--strip-bg); border: 1px solid var(--line);
  border-radius: var(--radius-sm); padding: 10px 14px;
  display: flex; flex-direction: column; gap: 6px; font-size: 12px;
}
.meta-item { display: flex; align-items: center; justify-content: space-between; }
.meta-label { color: var(--faint); }
.meta-val { color: var(--ink); }

.days-pill {
  font-family: var(--mono); font-weight: 600; font-size: 11px;
  padding: 2px 7px; border-radius: 4px;
  background: rgba(16, 185, 129, 0.15); color: var(--ok);
}
.days-pill.warn { background: rgba(245, 158, 11, 0.15); color: var(--warn); }

.card-footer {
  display: flex; align-items: center; justify-content: space-between;
  border-top: 1px solid var(--line); padding-top: 10px; font-size: 11px;
}
.auto-renew-chip { display: flex; align-items: center; gap: 5px; color: var(--muted); }
.ok-icon { color: var(--ok); }
.actions { display: flex; gap: 4px; }
.text-bad { color: var(--bad) !important; }

/* Modal */
.modal-backdrop {
  position: fixed; inset: 0; background: rgba(0,0,0,0.65);
  backdrop-filter: blur(8px); -webkit-backdrop-filter: blur(8px);
  z-index: 1050; display: flex; align-items: center; justify-content: center; padding: 20px;
}
.modal-dialog {
  width: 100%; max-width: 480px; background: var(--card-bg);
  border: 1px solid var(--card-border-hover); border-radius: var(--radius-card);
  backdrop-filter: blur(30px); -webkit-backdrop-filter: blur(30px);
  box-shadow: 0 25px 60px rgba(0, 0, 0, 0.6), var(--inner-highlight);
  overflow: hidden;
}
.modal-header { padding: 18px 20px; border-bottom: 1px solid var(--line); display: flex; align-items: center; justify-content: space-between; }
.modal-title { font-size: 16px; font-weight: 700; color: var(--ink); }
.close-modal-btn { color: var(--muted); }
.modal-body { padding: 20px; display: flex; flex-direction: column; gap: 14px; }
.form-group { display: flex; flex-direction: column; gap: 6px; }
.form-label { font-size: 12px; font-weight: 600; color: var(--ink-secondary); }
.modal-footer { display: flex; justify-content: flex-end; gap: 10px; padding-top: 8px; }

@media (max-width: 1100px) { .certs-grid { grid-template-columns: repeat(2, 1fr); } }
@media (max-width: 700px) { .certs-grid { grid-template-columns: 1fr; } }
</style>
