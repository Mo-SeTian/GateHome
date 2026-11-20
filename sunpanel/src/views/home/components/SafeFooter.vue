<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ html?: string }>()

const parts = computed(() => {
  // A template keeps even legacy markup inert. Only text and reconstructed links reach the page.
  const source = document.createElement('template')
  source.innerHTML = typeof props.html === 'string' ? props.html : ''
  const parts: { text: string; href?: string }[] = []
  const walker = document.createTreeWalker(source.content, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      if (node.parentElement?.closest('a'))
        return NodeFilter.FILTER_REJECT
      if (node instanceof Element && (!['DIV', 'P', 'SPAN', 'A', 'BR', 'STRONG', 'EM', 'B', 'I', 'SMALL'].includes(node.tagName)
        || node.namespaceURI !== 'http://www.w3.org/1999/xhtml'))
        return NodeFilter.FILTER_REJECT
      return NodeFilter.FILTER_ACCEPT
    },
  })
  while (walker.nextNode()) {
    const node = walker.currentNode
    if (node.nodeType === Node.TEXT_NODE) {
      parts.push({ text: node.textContent || '' })
    } else if (node instanceof Element && node.tagName === 'A') {
      const text = node.textContent || ''
      let href: string | undefined
      try {
        const raw = node.getAttribute('href')
        if (raw) {
          const url = new URL(raw, window.location.href)
          if (['https:', 'http:'].includes(url.protocol))
            href = url.href
        }
      } catch { /* Keep the label of an invalid link. */ }
      parts.push({ text, href })
    } else if (node instanceof Element && (node.tagName === 'BR' || (parts.length && ['DIV', 'P'].includes(node.tagName)))) {
      parts.push({ text: '\n' })
    }
  }
  return parts
})
</script>

<template>
  <div class="mt-5 footer whitespace-pre-line text-center text-slate-300">
    <span v-for="(part, index) in parts" :key="index">
      <a v-if="part.href" :href="part.href" target="_blank" rel="noopener noreferrer">{{ part.text }}</a>
      <template v-else>{{ part.text }}</template>
    </span>
  </div>
</template>
