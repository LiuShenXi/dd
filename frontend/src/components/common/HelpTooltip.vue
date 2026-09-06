<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, useTemplateRef, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  content?: string
  trigger?: 'hover' | 'click' | 'both'
  widthClass?: string
}>(), {
  trigger: 'hover',
  widthClass: 'w-64',
})

const show = ref(false)
const triggerRef = useTemplateRef<HTMLElement>('trigger')
const tooltipRef = useTemplateRef<HTMLElement>('tooltip')
const tooltipStyle = ref({ top: '0px', left: '0px' })
const placement = ref<'top' | 'bottom'>('top')

function openTooltip() {
  show.value = true
  nextTick(updatePosition)
}

function closeTooltip() {
  show.value = false
}

function onEnter() {
  if (props.trigger !== 'hover' && props.trigger !== 'both') return
  openTooltip()
}

function isInside(container: HTMLElement | null, target: EventTarget | null): boolean {
  return target instanceof Node && !!container?.contains(target)
}

// 悬停模式下指针在触发图标与提示框之间往返时保持打开，便于选中提示里的文字。
function onLeave(event: MouseEvent) {
  if (props.trigger !== 'hover' && props.trigger !== 'both') return
  if (isInside(tooltipRef.value, event.relatedTarget)) return
  closeTooltip()
}

function onTooltipLeave(event: MouseEvent) {
  if (props.trigger !== 'hover' && props.trigger !== 'both') return
  if (isInside(triggerRef.value, event.relatedTarget)) return
  closeTooltip()
}

function onClick(event: MouseEvent) {
  const hasHover = typeof window.matchMedia === 'function' && window.matchMedia('(hover: hover)').matches
  if (props.trigger !== 'click' && (props.trigger !== 'both' || hasHover)) return
  event.stopPropagation()
  if (props.trigger === 'both') {
    openTooltip()
    return
  }
  if (show.value) {
    closeTooltip()
    return
  }
  openTooltip()
}

function onDocumentClick(event: MouseEvent) {
  if ((props.trigger !== 'click' && props.trigger !== 'both') || !show.value) return
  const target = event.target as Node | null
  if (!target) return
  if (triggerRef.value?.contains(target) || tooltipRef.value?.contains(target)) return
  closeTooltip()
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (props.trigger !== 'click' && props.trigger !== 'both') return
  if (event.key === 'Escape') {
    closeTooltip()
  }
}

function onViewportChange() {
  if (!show.value) return
  updatePosition()
}

function updatePosition() {
  const el = triggerRef.value
  if (!el) return
  const rect = el.getBoundingClientRect()
  const tooltipWidth = tooltipRef.value?.getBoundingClientRect().width || 256
  const tooltipHeight = tooltipRef.value?.getBoundingClientRect().height || 80
  const viewportPadding = 8
  const preferredLeft = rect.left + rect.width / 2
  const left = Math.min(window.innerWidth - tooltipWidth / 2 - viewportPadding, Math.max(tooltipWidth / 2 + viewportPadding, preferredLeft))
  const topSpace = rect.top - viewportPadding
  const bottomSpace = window.innerHeight - rect.bottom - viewportPadding
  placement.value = topSpace >= tooltipHeight + 8 || topSpace >= bottomSpace ? 'top' : 'bottom'
  const top = placement.value === 'top'
    ? Math.max(tooltipHeight + viewportPadding, rect.top - 8)
    : Math.min(window.innerHeight - tooltipHeight - viewportPadding, rect.bottom + 8)
  tooltipStyle.value = { top: `${top}px`, left: `${left}px` }
}

function onFocusIn() {
  openTooltip()
}

function onFocusOut(event: FocusEvent) {
  if (isInside(tooltipRef.value, event.relatedTarget)) return
  closeTooltip()
}

onMounted(() => {
  document.addEventListener('click', onDocumentClick, true)
  document.addEventListener('keydown', onDocumentKeydown)
  window.addEventListener('resize', onViewportChange)
  window.addEventListener('scroll', onViewportChange, true)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', onDocumentClick, true)
  document.removeEventListener('keydown', onDocumentKeydown)
  window.removeEventListener('resize', onViewportChange)
  window.removeEventListener('scroll', onViewportChange, true)
})
</script>

<template>
  <div
    ref="trigger"
    class="group relative ml-1 inline-flex items-center align-middle"
    @mouseenter="onEnter"
    @mouseleave="onLeave"
    @click="onClick"
    @focusin="onFocusIn"
    @focusout="onFocusOut"
  >
    <!-- Trigger Icon -->
    <slot name="trigger">
      <svg
        class="h-4 w-4 cursor-help text-gray-400 transition-colors hover:text-primary-600 dark:text-gray-500 dark:hover:text-primary-400"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
        />
      </svg>
    </slot>

    <!-- Teleport to body to escape modal overflow clipping -->
    <Teleport to="body">
      <!-- before: 伪元素向下延伸一段透明区域，盖住提示框与触发图标之间的空隙，让指针能连续移入提示框。 -->
      <div
        ref="tooltip"
        v-show="show"
        role="tooltip"
        :class="[
          'fixed z-[99999] -translate-x-1/2 rounded-lg bg-gray-900 p-3 text-xs leading-relaxed text-white shadow-xl ring-1 ring-white/10 selection:bg-primary-200 selection:text-gray-900 before:absolute before:inset-x-0 dark:bg-gray-800 dark:selection:bg-primary-200 dark:selection:text-gray-900',
          placement === 'top' ? '-translate-y-full before:top-full before:h-3' : 'before:bottom-full before:h-3',
          props.widthClass,
        ]"
        :style="tooltipStyle"
        @mouseleave="onTooltipLeave"
      >
        <button
          v-if="props.trigger === 'click' || props.trigger === 'both'"
          type="button"
          class="absolute right-1.5 top-1.5 rounded p-1 text-gray-300 transition-colors hover:bg-white/10 hover:text-white"
          :aria-label="t('common.close')"
          @click.stop="closeTooltip"
        >
          <Icon name="x" size="sm" />
        </button>
        <slot>{{ content }}</slot>
        <div class="absolute left-1/2 h-2 w-2 -translate-x-1/2 rotate-45 bg-gray-900 dark:bg-gray-800" :class="placement === 'top' ? '-bottom-1' : '-top-1'"></div>
      </div>
    </Teleport>
  </div>
</template>
