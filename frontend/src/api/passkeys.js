import { request } from './client'

const path = '/api/account/passkeys'
export const passkeySupported = () => Boolean(window.isSecureContext &&
  (location.protocol === 'https:' || location.hostname === 'localhost') &&
  !/^[\d.]+$|:/.test(location.hostname) &&
  window.PublicKeyCredential?.parseCreationOptionsFromJSON &&
  window.PublicKeyCredential?.parseRequestOptionsFromJSON &&
  window.PublicKeyCredential?.prototype.toJSON)

export async function usePasskey(operation, signal) {
  const endpoint = path + '/' + operation
  const { publicKey } = await request(endpoint + '/begin', { method: 'POST', body: {}, signal, notifyUnauthorized: operation !== 'login' })
  let result
  try {
    if (operation === 'register') {
      result = await navigator.credentials.create({
        publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(publicKey), signal
      })
    } else {
      result = await navigator.credentials.get({
        publicKey: PublicKeyCredential.parseRequestOptionsFromJSON(publicKey), signal
      })
    }
    if (!result) throw new Error()
  } catch (err) {
    if (signal?.aborted) throw err
    throw new Error('通行密钥操作未完成，请重试')
  }
  return request(endpoint + '/finish', { method: 'POST', body: result.toJSON(), signal, notifyUnauthorized: operation !== 'login' })
}

export const listPasskeys = signal => request(path, { signal })
export const removePasskey = (id, signal) => request(path + '/' + encodeURIComponent(id), { method: 'DELETE', body: {}, signal })
