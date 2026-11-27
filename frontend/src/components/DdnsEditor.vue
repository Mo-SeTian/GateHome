<template>
  <dialog
    ref="dialog"
    class="report-dialog"
    aria-labelledby="ddns-title"
    @close="close"
    @cancel="blockBusyClose"
  >
    <header>
      <h2 id="ddns-title">{{ hostsOnly ? '编辑域名' : group ? '编辑 DDNS 组' : '添加 DDNS 组' }}</h2>
      <button class="btn btn-ghost" :disabled="isSavingConfig" aria-label="关闭" @click="dialog.close()">
        <X :size="18" />
      </button>
    </header>
    <form id="ddns-editor-form" @submit.prevent="save">
      <p v-if="error" ref="errorElement" class="report-error" role="alert" tabindex="-1">{{ error }}</p>
      <fieldset :disabled="isSavingConfig">
        <template v-if="!hostsOnly">
          <label class="form-field"
            >DNS 服务商<select v-model="draft.provider" name="provider" class="form-select">
              <option v-for="item in DNS_PROVIDERS" :key="item.id" :value="item.id">{{ item.name }}</option>
            </select></label
          >
          <label class="form-field"
            >组名称<input
              v-model="draft.name"
              name="name"
              class="form-input"
              maxlength="100"
              required
          /></label>
          <label class="form-field"
            >根域名 / DNS Zone<input
              v-model="draft.zone"
              name="zone"
              class="form-input mono-text"
              placeholder="example.com"
              required
          /></label>
          <SecretInput
            v-if="providerMeta.requiresKeyId"
            id="ddns-key-id"
            v-model="keyId"
            name="key_id"
            :label="providerMeta.keyIdLabel"
            :secret-name="providerMeta.keyIdSecretName"
            kind="ddns-key"
            :credential-id="group?.id || ''"
            :configured="keyConfigured"
            :placeholder="keyConfigured ? '已保存，点击小眼睛查看' : `填写 ${providerMeta.keyIdLabel}`"
          />
          <SecretInput
            id="ddns-token"
            v-model="token"
            name="token"
            :label="providerMeta.tokenLabel"
            :secret-name="providerMeta.tokenSecretName"
            kind="ddns"
            :credential-id="group?.id || ''"
            :configured="tokenConfigured"
            :placeholder="tokenConfigured ? '已保存，点击小眼睛查看' : providerMeta.requiresKeyId ? `填写 ${providerMeta.tokenLabel}` : '填写本组的 Token'"
          />
          <p class="report-note">{{ providerMeta.ddnsHint }}</p>
          <label class="form-field"
            >同步类型<select v-model="draft.mode" name="mode" class="form-select">
              <option value="ipv4">IPv4 · A</option>
              <option value="ipv6">IPv6 · AAAA</option>
              <option value="dual">IPv4 + IPv6</option>
            </select></label
          >
        </template>
        <label class="form-field"
          >子域名前缀<textarea
            v-model="hostsText"
            name="hosts"
            class="form-input mono-text"
            rows="4"
            placeholder="@, nas, app.home"
            required
          />
        </label>
        <p class="report-note">@ 表示根域名；支持逗号或换行。绑定此 DDNS 组的反代域名须保留。</p>
        <p class="domain-preview mono-text">{{ preview }}</p>
        <template v-if="!hostsOnly">
          <label class="form-field"
            >检查间隔 秒<input
              v-model.number="draft.interval"
              name="interval"
              class="form-input"
              type="number"
              min="60"
              max="86400"
              required
          /></label>
          <label class="toggle-row">
            <span>启用此组自动同步</span>
            <input v-model="draft.enabled" name="enabled" type="checkbox" />
          </label>
          <IPSourceFields :settings="draft" :interfaces="interfaces" endpoints />
          <p class="report-note">
            IPv4 / IPv6 接口各须 1–20 个。仅 DNS /
            灰云记录可自动修改；双栈中一类失败不影响另一类同步，手动任务有 5 秒冷却。
          </p>
        </template>
      </fieldset>
    </form>
    <footer>
      <button type="button" class="btn btn-secondary" :disabled="isSavingConfig" @click="dialog.close()">取消</button>
      <button type="submit" class="btn btn-primary" form="ddns-editor-form" :disabled="isSavingConfig">
        {{ isSavingConfig ? '保存中…' : '保存 DDNS 组' }}
      </button>
    </footer>
  </dialog>
</template>
<script setup>
import { newID } from '@/api/ids'
import { computed, nextTick, onMounted, onUnmounted, ref, toRaw, watch } from 'vue'
import { X } from 'lucide-vue-next'
import IPSourceFields from '@/components/IPSourceFields.vue'
import SecretInput from '@/components/SecretInput.vue'
import { DNS_PROVIDERS, credentialChange, ddnsHosts, ddnsPrefixes, dnsProviderMeta, replaceDDNS } from '@/api/dns'
import { useGatewayStatus } from '@/composables/useGatewayStatus'
const props = defineProps({
  snapshot: Object,
  group: Object,
  hostsOnly: Boolean,
  interfaces: { type: Array, default: () => [] },
  defaults: Object
})
const emit = defineEmits(['close', 'saved'])
const { saveConfig, isSavingConfig } = useGatewayStatus()
const draft = ref(
  props.group
    ? structuredClone(props.group)
    : {
        id: newID(),
        name: '',
        provider: 'cloudflare',
        zone: '',
        mode: 'dual',
        enabled: false,
        interval: 300,
        hosts: []
      }
)
for (const family of ['ipv4', 'ipv6']) {
  draft.value[family + '_source'] ||= 'url'
  draft.value[family + 'Text'] = (
    draft.value[family + '_urls'] ??
    (draft.value[family + '_url'] ? [draft.value[family + '_url']] : (props.defaults?.[family] ?? []))
  ).join('\n')
}
draft.value.interface ||= ''
const hostsText = ref(props.group ? ddnsPrefixes(props.group) : ''),
  keyId = ref(),
  token = ref()
const dialog = ref(null),
  error = ref(''),
  errorElement = ref(null)
const providerMeta = computed(() => dnsProviderMeta(draft.value.provider))
const sameProvider = computed(() => !!props.group && props.group.provider === draft.value.provider)
const keyConfigured = computed(() => sameProvider.value && !!props.snapshot.dns_key_configured?.[props.group?.id])
const tokenConfigured = computed(() =>
  sameProvider.value && !!(props.snapshot.dns_token_configured?.[props.group?.id] ?? props.snapshot.dns_credentials_configured?.[props.group?.id])
)
watch(() => draft.value.provider, () => {
  keyId.value = undefined
  token.value = undefined
})
const preview = computed(() => {
  try {
    return ddnsHosts(draft.value.zone, hostsText.value).join('\n')
  } catch (err) {
    return err.message
  }
})
function close() {
  keyId.value = undefined
  token.value = undefined
  emit('close')
}
function blockBusyClose(event) {
  if (isSavingConfig.value) event.preventDefault()
}
async function save() {
  if (isSavingConfig.value) return
  error.value = ''
  try {
    const record = props.hostsOnly ? structuredClone(props.group) : structuredClone(toRaw(draft.value))
    record.zone = record.zone.trim().toLowerCase()
    record.name = record.name.trim()
    record.hosts = ddnsHosts(record.zone, hostsText.value)
    if (!props.hostsOnly)
      for (const family of ['ipv4', 'ipv6']) {
        record[family + '_urls'] = record[family + 'Text']
          .split('\n')
          .map((item) => item.trim())
          .filter(Boolean)
        delete record[family + 'Text']
        delete record[family + '_url']
      }
    await saveConfig(
      props.snapshot,
      replaceDDNS(props.snapshot, record, props.group?.id ?? null),
      props.hostsOnly
        ? {}
        : credentialChange('dns_tokens', 'dns_credentials', record.id, { keyId: keyId.value, token: token.value }, providerMeta.value.requiresKeyId)
    )
    keyId.value = undefined
    token.value = undefined
    emit('saved')
  } catch (err) {
    error.value = err.message
    await nextTick()
    errorElement.value?.focus()
  }
}
onMounted(() => dialog.value.showModal())
onUnmounted(() => {
  keyId.value = undefined
  token.value = undefined
})
</script>
<style scoped>
.domain-preview {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  padding: 12px;
  background: var(--strip-bg);
  border-radius: var(--radius-sm);
  font-size: 12px;
}
</style>
