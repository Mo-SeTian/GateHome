<template>
  <Teleport to="body">
    <dialog ref="dialog" class="modal-box" aria-label="验证管理员身份" @cancel.prevent="finish(false)">
      <form @submit.prevent="verifyPassword">
        <header class="modal-header"><h2>验证管理员身份</h2></header>
        <div class="modal-body">
          <label class="verification-field">当前管理密码
            <input v-model="password" class="form-input" type="password" required
              autocomplete="current-password" maxlength="72" autofocus />
          </label>
          <p v-if="error" class="report-error" role="alert">{{ error }}</p>
        </div>
        <footer class="modal-footer">
          <button type="button" class="btn btn-secondary" @click="finish(false)">取消</button>
          <button v-if="canPasskey" type="button" class="btn btn-secondary" :disabled="busy" @click="verifyPasskey">使用通行密钥</button>
          <button type="submit" class="btn btn-primary" :disabled="busy">验证密码</button>
        </footer>
      </form>
    </dialog>
  </Teleport>
</template>

<script setup>
import { onBeforeUnmount, ref } from 'vue'
import { adminApi } from '@/api/admin'
import { passkeySupported, usePasskey } from '@/api/passkeys'

const canPasskey = passkeySupported()
const dialog = ref(null), password = ref(''), error = ref(''), busy = ref(false)
let resolve, controller

function verify() {
  if (dialog.value.open) return Promise.resolve(false)
  password.value = error.value = ''
  dialog.value.showModal()
  return new Promise(done => { resolve = done })
}
function finish(verified) {
  controller?.abort()
  controller = undefined
  password.value = ''
  busy.value = false
  if (dialog.value?.open) dialog.value.close()
  resolve?.(verified)
  resolve = undefined
}
async function validate(action) {
  if (busy.value) return
  const attempt = new AbortController()
  controller = attempt
  busy.value = true
  error.value = ''
  try {
    await action(attempt.signal)
    if (!attempt.signal.aborted) finish(true)
  } catch (err) {
    if (!attempt.signal.aborted) error.value = err.message
  } finally {
    if (controller === attempt) busy.value = false
  }
}
const verifyPassword = () => validate(signal => adminApi.verifyAdmin(password.value, signal))
const verifyPasskey = () => validate(signal => usePasskey('verify', signal))
defineExpose({ verify })
onBeforeUnmount(() => finish(false))
</script>
<style scoped>
.verification-field { display: grid; gap: 8px; }
.modal-footer { flex-wrap: wrap; }
</style>
