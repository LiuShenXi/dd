<template>
  <div>
    <label class="input-label" :for="id">{{ t('admin.productConfig.rules') }}</label>
    <textarea
      :id="id"
      :value="text"
      rows="5"
      class="input font-normal"
      :placeholder="placeholder"
      @input="onInput(($event.target as HTMLTextAreaElement).value)"
    ></textarea>
    <p class="input-hint">{{ t('admin.productConfig.rulesHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'

// 每行一条规则；空行在保存时由后端丢弃，留空则首页使用默认文案
const props = defineProps<{ modelValue: string[]; placeholder?: string }>()
const emit = defineEmits<{ 'update:modelValue': [string[]] }>()

const { t } = useI18n()
const id = useId()
const text = computed(() => props.modelValue.join('\n'))

function onInput(value: string) {
  emit('update:modelValue', value.split('\n'))
}
</script>
