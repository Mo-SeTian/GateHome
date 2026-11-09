<template>
  <dialog
    ref="dialog"
    class="report-dialog"
    aria-labelledby="certificate-editor-title"
    @close="close"
    @cancel="blockBusyClose"
  >
    <header>
      <h2 id="certificate-editor-title">
        {{ settings ? 'SSL 证书配置' : request ? '编辑证书任务' : '添加证书任务' }}
      </h2>
      <button class="btn btn-ghost" aria-label="关闭" :disabled="isSavingConfig" @click="dialog.close()">
        <X :size="18" />
      </button>
    </header>
    <form @submit.prevent="save">
      <p v-if="error" ref="errorElement" class="report-error" role="alert" tabindex="-1">{{ error }}</p>
      <fieldset :disabled="isSavingConfig">
        <template v-if="settings">
          <label class="check-label"
            ><input v-model="draft.enabled" name="acme_enabled" type="checkbox" />启用自动申请与续期</label
          >
          <label
            >ACME 联系邮箱<input
              v-model="draft.email"
              name="email"
              class="form-input"
              type="email"
              :required="draft.enabled"
          /></label>
          <label
            >签发环境<select v-model="draft.staging" name="staging" class="form-select">
              <option :value="true">Let's Encrypt 测试环境</option>
              <option :value="false">Let's Encrypt 正式环境</option>
            </select></label
          >
          <label class="check-label"
            ><input v-model="draft.accept_terms" name="accept_terms" type="checkbox" />同意 Let's Encrypt
            服务条款</label
          >
          <p class="report-note">
            测试与正式证书、账户分开保存；切换环境只加载该环境的证书。每小时检查有效期，手动检查不会强制续签尚未到期的证书；失败后有
            5 分钟重试冷却。
          </p>
          <a
            class="report-note"
            href="https://letsencrypt.org/repository/"
            target="_blank"
            rel="noopener noreferrer"
            >查看服务条款</a
          >
        </template>
        <template v-else>
          <label
            >证书域名<textarea
              v-model="domainsText"
              name="domains"
              class="form-input mono-text"
              rows="4"
              placeholder="example.com, *.example.com"
              required
              autofocus
            />
          </label>
          <p class="report-note">
            逗号或换行分隔；多个域名申请为同一张证书。泛域名只覆盖一级子域名，根域名须单独填写。域名无需已有反代。
          </p>
          <label
            >DNS 验证服务商<select v-model="draft.provider" name="provider" class="form-select">
              <option value="cloudflare">Cloudflare · DNS-01</option>
            </select></label
          >
          <p class="report-note">
            使用 ECDSA P-256。独立 Token 需要相应 Zone 的 Zone:Read / DNS:Edit 权限，与 DDNS 组的凭据分开。
          </p>
          <label
            >独立 API Token<input
              v-model="token"
              name="token"
              class="form-input"
              type="password"
              autocomplete="new-password"
              maxlength="512"
              :placeholder="
                snapshot.certificate_credentials_configured?.[request?.id]
                  ? '已配置，留空保留'
                  : '填写此证书任务的 Token'
              "
          /></label>
          <label class="check-label"
            ><input v-model="clearToken" name="clear_token" type="checkbox" />清除已保存的 Token</label
          >
          <label class="check-label"
            ><input
              v-model="draft.enabled"
              name="request_enabled"
              type="checkbox"
            />启用此任务自动申请与续期</label
          >
        </template>
        <footer>
          <button type="button" class="btn btn-secondary" @click="dialog.close()">取消</button
          ><button type="submit" class="btn btn-primary">
            {{ isSavingConfig ? '保存中…' : '保存证书配置' }}
          </button>
        </footer>
      </fieldset>
    </form>
  </dialog>
</template>
<script setup>
import { nextTick, onMounted, onUnmounted, ref, toRaw } from 'vue'
import { X } from 'lucide-vue-next'
import { domainList, replaceCertificate, tokenChange } from '@/api/dns'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
const props = defineProps({ snapshot: Object, request: Object, settings: Boolean })
const emit = defineEmits(['close', 'saved'])
const { saveConfig, isSavingConfig } = useGatewayStatus()
const draft = ref(
  structuredClone(
    props.settings
      ? props.snapshot.config.acme
      : props.request || { id: crypto.randomUUID(), provider: 'cloudflare', domains: [], enabled: true }
  )
)
const domainsText = ref(props.request?.domains.join('\n') || ''),
  token = ref(''),
  clearToken = ref(false),
  error = ref(''),
  errorElement = ref(null),
  dialog = ref(null)
function blockBusyClose(event) {
  if (isSavingConfig.value) event.preventDefault()
}
function close() {
  token.value = ''
  emit('close')
}
async function save() {
  if (isSavingConfig.value) return
  error.value = ''
  try {
    const record = structuredClone(toRaw(draft.value))
    let config,
      credentials = {}
    if (props.settings) {
      config = structuredClone(props.snapshot.config)
      config.acme = { ...record, email: record.email.trim() }
    } else {
      record.domains = domainList(domainsText.value)
      config = replaceCertificate(props.snapshot, record, props.request?.id ?? null)
      credentials = tokenChange('certificate_tokens', record.id, token.value, clearToken.value)
    }
    await saveConfig(props.snapshot, config, credentials)
    token.value = ''
    emit('saved')
  } catch (err) {
    error.value = err.message
    await nextTick()
    errorElement.value?.focus()
  }
}
onMounted(() => dialog.value.showModal())
onUnmounted(() => {
  token.value = ''
})
</script>
<style scoped>
.check-label {
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>
