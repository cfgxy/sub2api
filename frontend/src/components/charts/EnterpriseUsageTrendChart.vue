<template>
  <div class="h-56">
    <Line :data="chartData" :options="lineOptions" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler
} from 'chart.js'
import { Line } from 'vue-chartjs'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler)

const props = defineProps<{
  /** 已有的时序序列：X 轴文案与 Y 轴数值由调用方按接口字段映射 */
  labels: string[]
  values: number[]
  datasetLabel: string
}>()

// 图表实例化色值只能是字面量，沿用原版 components/charts/* 的取色约定，不引入新调色板
const isDarkMode = ref(document.documentElement.classList.contains('dark'))
let themeObserver: MutationObserver | undefined
onMounted(() => {
  themeObserver = new MutationObserver(() => {
    isDarkMode.value = document.documentElement.classList.contains('dark')
  })
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
})
onUnmounted(() => themeObserver?.disconnect())
const chartColors = computed(() => ({
  text: isDarkMode.value ? '#e5e7eb' : '#374151',
  grid: isDarkMode.value ? '#374151' : '#e5e7eb',
  line: '#14b8a6'
}))

const chartData = computed(() => ({
  labels: props.labels,
  datasets: [
    {
      label: props.datasetLabel,
      data: props.values,
      borderColor: chartColors.value.line,
      backgroundColor: 'rgba(20, 184, 166, 0.12)',
      borderWidth: 2,
      pointRadius: 2,
      tension: 0.3,
      fill: true
    }
  ]
}))

const lineOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  plugins: {
    legend: { display: false },
    tooltip: { enabled: true }
  },
  scales: {
    x: {
      ticks: { color: chartColors.value.text, maxRotation: 0, autoSkip: true },
      grid: { display: false }
    },
    y: {
      beginAtZero: true,
      ticks: { color: chartColors.value.text, precision: 0 },
      grid: { color: chartColors.value.grid }
    }
  }
}))
</script>
