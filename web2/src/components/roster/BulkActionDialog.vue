<script setup lang="ts">
// Confirm → live-progress → done dialog for a bulk roster action.
//
//  • confirm: shows exactly which members the action will run on, and which
//    selected ones are skipped (with the reason — e.g. "running"). Destructive
//    actions gate the confirm behind a typed phrase (requireType).
//  • running: locks the dialog and runs; each member flips from a spinner to
//    ✓ / ✗ as its own request settles (driven by the run()'s onSettled hook),
//    with a live n/total counter. This is the "confirm, then watch it apply".
//  • done: shows the outcome and a Close button; closing emits the BulkResult.
import { ref, computed } from 'vue'
import EvDialog from '@/components/base/EvDialog.vue'
import EvButton from '@/components/base/EvButton.vue'
import EvSpinner from '@/components/base/EvSpinner.vue'
import type { BulkResult } from '@/stores/space'

const props = defineProps<{
  title: string
  verb: string // gerund shown while running, e.g. "Clearing"
  danger?: boolean
  members: string[] // the eligible targets (already filtered by the caller)
  skipped?: { name: string; reason: string }[] // selected-but-ineligible
  requireType?: string // type-to-confirm phrase; undefined = a plain confirm
  run: (onSettled: (name: string, error?: string) => void) => Promise<BulkResult>
}>()
const emit = defineEmits<{ done: [BulkResult]; cancel: [] }>()

type Status = 'running' | 'ok' | 'failed'
const phase = ref<'confirm' | 'running' | 'done'>('confirm')
const status = ref<Record<string, Status>>({})
const errors = ref<Record<string, string>>({})
const typed = ref('')
const result = ref<BulkResult | null>(null)

const canConfirm = computed(() => !props.requireType || typed.value.trim() === props.requireType)
const settledCount = computed(() => Object.values(status.value).filter((s) => s !== 'running').length)
const okCount = computed(() => (result.value ? result.value.ok.length : 0))
const failCount = computed(() => (result.value ? result.value.failed.length : 0))

async function start() {
  if (!canConfirm.value || phase.value !== 'confirm') return
  phase.value = 'running'
  props.members.forEach((m) => (status.value[m] = 'running'))
  result.value = await props.run((name, error) => {
    status.value[name] = error ? 'failed' : 'ok'
    if (error) errors.value[name] = error
  })
  phase.value = 'done'
}
// EvDialog emits `close` on Esc / scrim / ✕. We swallow it mid-flight (the
// fan-out can't be cancelled cleanly) and otherwise route confirm→cancel,
// done→commit the result.
function onClose() {
  if (phase.value === 'running') return
  if (phase.value === 'done' && result.value) emit('done', result.value)
  else emit('cancel')
}
</script>

<template>
  <EvDialog :title="title" width="30rem" @close="onClose">
    <template v-if="phase === 'confirm'">
      <p class="lead">
        Runs on <strong>{{ members.length }}</strong> member{{ members.length === 1 ? '' : 's' }}:
      </p>
      <div class="chips">
        <span v-for="m in members" :key="m" class="chip">{{ m }}</span>
      </div>
      <p v-if="skipped && skipped.length" class="skip">
        skipping {{ skipped.length }} —
        <span v-for="s in skipped" :key="s.name" class="skipitem">{{ s.name }} <em>({{ s.reason }})</em></span>
      </p>
      <div v-if="requireType" class="type">
        <label>Type <code>{{ requireType }}</code> to confirm</label>
        <input v-model="typed" :placeholder="requireType" @keyup.enter="start" />
      </div>
    </template>

    <template v-else>
      <p class="lead">
        {{ phase === 'running' ? `${verb}…` : 'Done' }}
        <span class="count">{{ settledCount }}/{{ members.length }}</span>
        <span v-if="phase === 'done'" class="tally">· {{ okCount }} ok<template v-if="failCount"> · {{ failCount }} failed</template></span>
      </p>
      <ul class="rows">
        <li v-for="m in members" :key="m" :class="status[m]">
          <span class="nm">{{ m }}</span>
          <EvSpinner v-if="status[m] === 'running'" :size="12" />
          <span v-else-if="status[m] === 'ok'" class="ok">✓</span>
          <span v-else class="fail" :title="errors[m]">✗ {{ errors[m] }}</span>
        </li>
      </ul>
    </template>

    <template #footer>
      <template v-if="phase === 'confirm'">
        <EvButton @click="emit('cancel')">Cancel</EvButton>
        <EvButton :variant="danger ? 'danger' : 'primary'" :disabled="!canConfirm" @click="start">
          {{ danger ? 'Confirm' : 'Run' }}
        </EvButton>
      </template>
      <EvButton v-else variant="primary" :loading="phase === 'running'" :disabled="phase === 'running'" @click="onClose">
        {{ phase === 'running' ? 'Running' : 'Close' }}
      </EvButton>
    </template>
  </EvDialog>
</template>

<style scoped>
.lead {
  font-size: var(--fs-sm);
  margin: 0 0 var(--sp-2);
}
.count {
  font-family: var(--font-mono);
  color: var(--color-text-muted);
  margin-left: 0.3rem;
}
.tally {
  color: var(--color-text-muted);
  margin-left: 0.3rem;
}
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem;
}
.chip {
  font-size: var(--fs-xs);
  border: 1px solid var(--color-line);
  border-radius: var(--r-sm);
  padding: 0.1rem 0.4rem;
  color: var(--color-text);
  background: var(--color-bg);
}
.skip {
  font-size: var(--fs-xs);
  color: var(--color-text-muted);
  margin: var(--sp-2) 0 0;
}
.skipitem {
  margin-right: 0.4rem;
}
.skipitem em {
  font-style: normal;
  opacity: 0.8;
}
.type {
  margin-top: var(--sp-3);
  display: grid;
  gap: 0.3rem;
  font-size: var(--fs-sm);
}
.type code {
  background: var(--color-surface-2);
  padding: 0 0.3rem;
  border-radius: var(--r-sm);
  color: var(--color-danger);
}
.type input {
  background: var(--color-bg);
  border: 1px solid var(--color-line);
  border-radius: var(--r-sm);
  color: var(--color-text);
  padding: 0.25rem 0.4rem;
  font-size: var(--fs-sm);
}
.rows {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 0.2rem;
  max-height: 18rem;
  overflow: auto;
}
.rows li {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  font-size: var(--fs-sm);
  padding: 0.15rem 0.3rem;
  border-radius: var(--r-sm);
}
.rows .nm {
  flex: 1;
  font-family: var(--font-mono);
  font-size: var(--fs-xs);
}
.rows li.ok {
  color: var(--color-text);
}
.rows .ok {
  color: var(--color-success, #2ea043);
}
.rows .fail {
  color: var(--color-danger);
  font-size: var(--fs-xs);
}
</style>
