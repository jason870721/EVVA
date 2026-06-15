<script setup lang="ts">
// The team roster (LEFT region). Calm member cards + all the composition dialogs
// (add agent, schedule, skills, external-events guide) and the remove confirm.
// Emits 'select' so the workspace opens the member's live stream + inspector;
// everything else is handled here against the space store.
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import { useSpaceStore, type BulkResult } from '@/stores/space'
import { errMsg } from '@/lib/util'
import { orderRoster } from '@/lib/events'
import type { MemberInfo } from '@/types/wire'
import MemberCard from './MemberCard.vue'
import BulkActionDialog from './BulkActionDialog.vue'
import AddAgentDialog from '@/components/compose/AddAgentDialog.vue'
import ScheduleEditor from '@/components/compose/ScheduleEditor.vue'
import SkillsPanel from '@/components/compose/SkillsPanel.vue'
import EventSources from '@/components/compose/EventSources.vue'
import ConfirmDialog from '@/components/safety/ConfirmDialog.vue'
import EvPanel from '@/components/base/EvPanel.vue'

const emit = defineEmits<{ select: [name: string] }>()
const route = useRoute()
const space = useSpaceStore()

const showAdd = ref(false)
const schedFor = ref<MemberInfo | null>(null)
const skillsFor = ref('')
const showEvents = ref(false)
const removing = ref('')
const clearing = ref('')
// bypass means fully autonomous — it alone goes through a confirm.
const bypassing = ref('')
const err = ref('')

// ── Roster ordering ─────────────────────────────────────────────────────────
// Always float the members you should look at to the top so a long roster never
// buries an active worker at the bottom — leader pinned, then needs-attention →
// busy → idle → suspended → frozen, alphabetical within a tier. The attention
// tier reuses space.attention (the same signal as the AttentionStrip), so a
// stalled member counts too. Reorders animate via the list's <TransitionGroup>.
const orderedMembers = computed(() => orderRoster(space.merged, space.attention.map((a) => a.name)))

// ── Multi-select / bulk ops ────────────────────────────────────────────────
// Selection is ephemeral (never in the URL — that's reserved for single-member
// inspect via ?m=). Entering select mode turns cards into checkbox rows and
// surfaces the bulk bar; leaving it clears the selection. Every action — even
// the safe ones — routes through BulkActionDialog: confirm what will run (and
// what is skipped), then watch each member settle.
type ActionId = 'clear' | 'micro' | 'full' | 'suspend' | 'resume' | 'freeze' | 'unfreeze'
type Settled = (name: string, error?: string) => void
// Past this many eligible members a destructive action (clear / full) demands a
// typed phrase, not just an OK.
const TYPE_CONFIRM_THRESHOLD = 4

interface BulkAction {
  label: string // bar button text (sans count)
  title: (n: number) => string // dialog heading
  verb: string // gerund shown while running
  danger: boolean
  group: 'data' | 'life'
  eligible: (m: MemberInfo) => boolean
  reason: (m: MemberInfo) => string // why a selected-but-ineligible member is skipped
  run: (names: string[], onSettled: Settled) => Promise<BulkResult>
  typePhrase?: string // type-to-confirm word when danger + over threshold
}

// clear / compact refuse a member with a run in flight (run === 'busy'); a
// suspended member has no run in flight, so it is eligible.
const idle = (m: MemberInfo) => m.run !== 'busy'
const ACTIONS: Record<ActionId, BulkAction> = {
  clear: {
    label: '🧹 clear', verb: 'Clearing', danger: true, group: 'data', typePhrase: 'clear',
    title: (n) => `Clear ${n} session${n === 1 ? '' : 's'}?`,
    eligible: idle, reason: () => 'running',
    run: (names, cb) => space.bulkClear(names, cb),
  },
  micro: {
    label: '🗜 micro', verb: 'Compacting', danger: false, group: 'data',
    title: (n) => `Compact ${n} member${n === 1 ? '' : 's'} (micro)?`,
    eligible: idle, reason: () => 'running',
    run: (names, cb) => space.bulkCompact(names, 'micro', cb),
  },
  full: {
    label: 'full…', verb: 'Compacting', danger: true, group: 'data', typePhrase: 'compact',
    title: (n) => `Full-compact ${n} member${n === 1 ? '' : 's'}?`,
    eligible: idle, reason: () => 'running',
    run: (names, cb) => space.bulkCompact(names, 'full', cb),
  },
  suspend: {
    label: '⏸ suspend', verb: 'Suspending', danger: false, group: 'life',
    title: (n) => `Suspend ${n} member${n === 1 ? '' : 's'}?`,
    eligible: (m) => m.run === 'busy', reason: (m) => `not running (${m.run})`,
    run: (names, cb) => space.bulkCmd('suspend', names, cb),
  },
  resume: {
    label: '▶ resume', verb: 'Resuming', danger: false, group: 'life',
    title: (n) => `Resume ${n} member${n === 1 ? '' : 's'}?`,
    eligible: (m) => m.run === 'suspended', reason: (m) => `not suspended (${m.run})`,
    run: (names, cb) => space.bulkCmd('resume', names, cb),
  },
  freeze: {
    label: '❄ freeze', verb: 'Freezing', danger: false, group: 'life',
    title: (n) => `Freeze ${n} member${n === 1 ? '' : 's'}?`,
    eligible: (m) => m.membership === 'active', reason: (m) => `already ${m.membership}`,
    run: (names, cb) => space.bulkCmd('freeze', names, cb),
  },
  unfreeze: {
    label: '▶❄ unfreeze', verb: 'Unfreezing', danger: false, group: 'life',
    title: (n) => `Unfreeze ${n} member${n === 1 ? '' : 's'}?`,
    eligible: (m) => m.membership === 'frozen', reason: (m) => `not frozen (${m.membership})`,
    run: (names, cb) => space.bulkCmd('unfreeze', names, cb),
  },
}
const DATA_IDS = (Object.keys(ACTIONS) as ActionId[]).filter((id) => ACTIONS[id].group === 'data')
const LIFE_IDS = (Object.keys(ACTIONS) as ActionId[]).filter((id) => ACTIONS[id].group === 'life')

const selectMode = ref(false)
const sel = ref<Set<string>>(new Set())
const summary = ref('')
const pending = ref<{ id: ActionId; names: string[]; skipped: { name: string; reason: string }[] } | null>(null)

const selected = computed(() => space.merged.filter((m) => sel.value.has(m.name)))
const busyCount = computed(() => selected.value.filter((m) => m.run === 'busy').length)
// Per-action split of the current selection into eligible names + skipped
// (ineligible) members with the reason — computed once, read by both the bar
// counts and the dialog.
const plan = computed(() => {
  const out = {} as Record<ActionId, { eligible: string[]; skipped: { name: string; reason: string }[] }>
  for (const id of Object.keys(ACTIONS) as ActionId[]) {
    const a = ACTIONS[id]
    const eligible: string[] = []
    const skipped: { name: string; reason: string }[] = []
    for (const m of selected.value) {
      if (a.eligible(m)) eligible.push(m.name)
      else skipped.push({ name: m.name, reason: a.reason(m) })
    }
    out[id] = { eligible, skipped }
  }
  return out
})
const requireTypeFor = computed(() => {
  const p = pending.value
  if (!p) return undefined
  const a = ACTIONS[p.id]
  return a.danger && p.names.length >= TYPE_CONFIRM_THRESHOLD ? a.typePhrase : undefined
})

function toggleSelect() {
  if (selectMode.value) exitSelect()
  else selectMode.value = true
}
function exitSelect() {
  selectMode.value = false
  sel.value = new Set()
  summary.value = ''
}
function toggle(name: string) {
  const s = new Set(sel.value)
  if (s.has(name)) s.delete(name)
  else s.add(name)
  sel.value = s
}
// Master select-all checkbox (top of the roster in select mode): tri-state —
// ticked when every member is selected, a dash when some are, empty when none.
const allSelected = computed(() => space.merged.length > 0 && sel.value.size === space.merged.length)
const someSelected = computed(() => sel.value.size > 0 && !allSelected.value)
function toggleAll() {
  sel.value = allSelected.value ? new Set() : new Set(space.merged.map((m) => m.name))
}
function selectIdle() {
  sel.value = new Set(space.merged.filter(idle).map((m) => m.name))
}
function ask(id: ActionId) {
  const { eligible, skipped } = plan.value[id]
  if (!eligible.length) return
  summary.value = ''
  pending.value = { id, names: eligible, skipped }
}
// Passed to the dialog as its `run`; it drives the snapshot captured in `ask`.
function runPending(onSettled: Settled): Promise<BulkResult> {
  const p = pending.value
  if (!p) return Promise.resolve({ ok: [], failed: [] })
  return ACTIONS[p.id].run(p.names, onSettled)
}
function onBulkDone(r: BulkResult) {
  const id = pending.value?.id
  pending.value = null
  if (!id) return
  // Drop the members that succeeded from the selection; leave the ones that
  // failed (and any that were skipped) ticked so the operator can retry them
  // directly without re-selecting.
  if (r.ok.length) {
    const s = new Set(sel.value)
    r.ok.forEach((n) => s.delete(n))
    sel.value = s
  }
  summary.value =
    `${ACTIONS[id].verb.toLowerCase()} done · ${r.ok.length} ok` +
    (r.failed.length ? ` · ${r.failed.length} failed (kept selected)` : '')
}

// Esc leaves select mode — but only when no dialog is up (EvDialog owns Esc
// while open, and the listener is no-op'd via the !pending guard).
function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && selectMode.value && !pending.value) exitSelect()
}
watch(selectMode, (on) => {
  if (on) window.addEventListener('keydown', onKey)
  else window.removeEventListener('keydown', onKey)
})
onBeforeUnmount(() => window.removeEventListener('keydown', onKey))

async function cmd(verb: 'freeze' | 'unfreeze' | 'suspend' | 'resume', name: string) {
  try {
    await space.memberCmd(verb, name)
  } catch (e) {
    err.value = errMsg(e)
  }
}
async function onSetSchedule(d: { cron: string; prompt: string }) {
  const name = schedFor.value?.name
  schedFor.value = null
  if (!name) return
  try {
    await space.setSchedule(name, d.cron, d.prompt)
  } catch (e) {
    err.value = errMsg(e)
  }
}
async function onClearSchedule() {
  const name = schedFor.value?.name
  schedFor.value = null
  if (!name) return
  try {
    await space.clearSchedule(name)
  } catch (e) {
    err.value = errMsg(e)
  }
}
async function doRemove(deleteDir: boolean) {
  const name = removing.value
  removing.value = ''
  if (!name) return
  try {
    await space.removeMember(name, deleteDir)
  } catch (e) {
    err.value = errMsg(e)
  }
}
async function doClear() {
  const name = clearing.value
  clearing.value = ''
  if (!name) return
  try {
    await space.clearMember(name)
  } catch (e) {
    err.value = errMsg(e) // 409 busy lands here: "suspend it or wait"
  }
}
async function applyPermMode(name: string, mode: string) {
  try {
    await space.setPermissionMode(name, mode)
  } catch (e) {
    err.value = errMsg(e)
  }
}
function onPermMode(name: string, mode: string) {
  if (mode === 'bypass') {
    bypassing.value = name
    return
  }
  void applyPermMode(name, mode)
}
async function doBypass() {
  const name = bypassing.value
  bypassing.value = ''
  if (!name) return
  await applyPermMode(name, 'bypass')
}
</script>

<template>
  <EvPanel class="rosterp">
    <template #header>
      <span class="title">Roster</span>
      <div class="hactions">
        <button class="hbtn" :class="{ on: selectMode }" title="select multiple members" @click="toggleSelect">
          {{ selectMode ? '✗ done' : '✓ select' }}
        </button>
        <button class="hbtn" title="external events webhook" @click="showEvents = true">⚡</button>
        <button class="hbtn" @click="showAdd = true">+ add</button>
      </div>
    </template>

    <div v-if="selectMode" class="bulkbar">
      <label class="brow meta">
        <input
          type="checkbox"
          class="pick"
          :checked="allSelected"
          :indeterminate.prop="someSelected"
          :aria-label="allSelected ? 'deselect all' : 'select all'"
          @change="toggleAll"
        />
        <span class="salabel">{{ sel.size }} / {{ space.merged.length }} selected<template v-if="busyCount"> · {{ busyCount }} running</template></span>
        <span class="spacer" />
        <button class="chip" :disabled="!space.merged.length" @click="selectIdle">idle</button>
      </label>
      <template v-if="sel.size">
        <div class="brow">
          <span class="glabel">data</span>
          <button
            v-for="id in DATA_IDS"
            :key="id"
            class="b"
            :class="{ risky: ACTIONS[id].danger }"
            :disabled="!plan[id].eligible.length"
            @click="ask(id)"
          >
            {{ ACTIONS[id].label }} ({{ plan[id].eligible.length }})
          </button>
        </div>
        <div class="brow">
          <span class="glabel">life</span>
          <button
            v-for="id in LIFE_IDS"
            :key="id"
            class="b"
            :disabled="!plan[id].eligible.length"
            @click="ask(id)"
          >
            {{ ACTIONS[id].label }} ({{ plan[id].eligible.length }})
          </button>
        </div>
        <p v-if="summary" class="bsum">{{ summary }}</p>
      </template>
      <p v-else class="bsum">tick members below to act on them</p>
    </div>

    <TransitionGroup tag="ul" name="rosters" class="list">
      <MemberCard
        v-for="m in orderedMembers"
        :key="m.name"
        :member="m"
        :selected="route.query.m === m.name"
        :now="space.now"
        :select-mode="selectMode"
        :checked="sel.has(m.name)"
        :busy="space.memberBusy(m.name)"
        @toggle="toggle(m.name)"
        @select="emit('select', m.name)"
        @freeze="cmd('freeze', m.name)"
        @unfreeze="cmd('unfreeze', m.name)"
        @suspend="cmd('suspend', m.name)"
        @resume="cmd('resume', m.name)"
        @schedule="schedFor = m"
        @skills="skillsFor = m.name"
        @clear="clearing = m.name"
        @remove="removing = m.name"
        @perm-mode="(mode: string) => onPermMode(m.name, mode)"
      />
    </TransitionGroup>
    <p v-if="!orderedMembers.length" class="dim">no members yet</p>
    <p v-if="err" class="err">{{ err }}</p>

    <BulkActionDialog
      v-if="pending"
      :title="ACTIONS[pending.id].title(pending.names.length)"
      :verb="ACTIONS[pending.id].verb"
      :danger="ACTIONS[pending.id].danger"
      :members="pending.names"
      :skipped="pending.skipped"
      :require-type="requireTypeFor"
      :run="runPending"
      @done="onBulkDone"
      @cancel="pending = null"
    />

    <AddAgentDialog v-if="showAdd" @created="showAdd = false" @cancel="showAdd = false" />
    <ScheduleEditor
      v-if="schedFor"
      :member="schedFor.name"
      :cron="schedFor.cron"
      :prompt="schedFor.schedulePrompt"
      @set="onSetSchedule"
      @clear="onClearSchedule"
      @cancel="schedFor = null"
    />
    <SkillsPanel v-if="skillsFor" :member="skillsFor" @close="skillsFor = ''" />
    <EventSources v-if="showEvents" @close="showEvents = false" />
    <ConfirmDialog
      v-if="removing"
      :title="`Remove ${removing}?`"
      :message="`${removing} stops running and the leader is asked to reassign its tasks. History is kept.`"
      confirm-label="Remove"
      :danger="true"
      checkbox-label="Also delete its on-disk definition (cannot be re-added without recreating)"
      @confirm="doRemove"
      @cancel="removing = ''"
    />
    <ConfirmDialog
      v-if="clearing"
      :title="`Clear ${clearing}'s session?`"
      :message="`${clearing} starts over with a blank context — its conversation history is wiped (a busy member refuses; suspend it first). Schedule, skills, and memory files are kept.`"
      confirm-label="Clear session"
      :danger="true"
      @confirm="doClear"
      @cancel="clearing = ''"
    />
    <ConfirmDialog
      v-if="bypassing"
      :title="`Set ${bypassing} to bypass?`"
      :message="`${bypassing} runs fully autonomous — every tool call executes without approval (deny rules still bind). Applies immediately, mid-run included, and survives restarts until the swarm is freshly re-registered.`"
      confirm-label="Set bypass"
      :danger="true"
      @confirm="doBypass"
      @cancel="bypassing = ''"
    />
  </EvPanel>
</template>

<style scoped>
.rosterp {
  min-height: 0;
}
.hactions {
  display: flex;
  gap: var(--sp-1);
}
.hbtn {
  font-size: var(--fs-xs);
  padding: 0.1rem 0.45rem;
  background: transparent;
  border: 1px dashed var(--color-line);
  border-radius: var(--r-md);
  color: var(--color-text-muted);
  cursor: pointer;
}
.hbtn:hover {
  border-color: var(--color-accent);
  color: var(--color-text);
}
.list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: var(--sp-2);
}
/* FLIP-animate cards sliding to their new slot when the activity sort reorders
   them, and fade new members in, so the reshuffle reads as motion not a jump. */
.rosters-move {
  transition: transform 0.28s var(--ease-out, ease);
}
.rosters-enter-active {
  transition: opacity 0.28s var(--ease-out, ease);
}
.rosters-enter-from {
  opacity: 0;
}
.dim {
  color: var(--color-text-muted);
  font-size: var(--fs-sm);
}
.err {
  color: var(--color-danger);
  font-size: var(--fs-xs);
  margin-top: var(--sp-2);
}
.hbtn.on {
  border-style: solid;
  border-color: var(--color-accent);
  color: var(--color-text);
}
/* Pinned at the TOP of the roster in select mode: the select-all row + the
   action groups stay reachable no matter how long the member list scrolls. */
.bulkbar {
  position: sticky;
  top: 0;
  z-index: 5;
  margin-bottom: var(--sp-2);
  padding: var(--sp-2);
  display: grid;
  gap: 0.3rem;
  background: var(--color-surface);
  border: 1px solid var(--color-accent);
  border-radius: var(--r-md);
}
.brow {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  flex-wrap: wrap;
}
/* Select-all row, the bulkbar's first line — divided from the action groups. */
.brow.meta {
  gap: 0.5rem;
  padding-bottom: 0.35rem;
  margin-bottom: 0.1rem;
  border-bottom: 1px solid var(--color-line);
  color: var(--color-text-muted);
  cursor: pointer;
}
.spacer {
  flex: 1;
}
.glabel {
  font-size: var(--fs-xs);
  color: var(--color-text-muted);
  width: 2.2rem;
  flex-shrink: 0;
}
.bulkbar .b,
.chip {
  background: var(--color-bg);
  border: 1px solid var(--color-line);
  border-radius: var(--r-sm);
  color: var(--color-text);
  cursor: pointer;
  font-size: var(--fs-xs);
  padding: 0.15rem 0.4rem;
  white-space: nowrap;
}
.bulkbar .b:hover:not(:disabled),
.chip:hover:not(:disabled) {
  border-color: var(--color-accent);
}
.bulkbar .b:disabled,
.chip:disabled {
  opacity: 0.45;
  cursor: default;
}
.bulkbar .b.risky:not(:disabled) {
  color: var(--color-danger);
  border-color: color-mix(in srgb, var(--color-danger) 45%, transparent);
}
.bsum {
  font-size: var(--fs-xs);
  color: var(--color-text-muted);
  margin: 0.1rem 0 0;
}
.salabel {
  font-family: var(--font-mono);
}
/* Master checkbox: same custom look as the cards (MemberCard .pick), plus an
   indeterminate dash for a partial selection. */
.pick {
  appearance: none;
  -webkit-appearance: none;
  width: 1.1rem;
  height: 1.1rem;
  flex-shrink: 0;
  margin: 0;
  border: 2px solid var(--color-line-strong);
  border-radius: var(--r-sm);
  background: var(--color-bg);
  cursor: pointer;
  position: relative;
  transition: background var(--dur-fast) var(--ease-out), border-color var(--dur-fast) var(--ease-out);
}
.pick:hover {
  border-color: var(--color-accent);
}
.pick:checked,
.pick:indeterminate {
  background: var(--color-accent);
  border-color: var(--color-accent);
}
.pick:checked::after,
.pick:indeterminate::after {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  line-height: 1;
  color: var(--btn-primary-fg);
}
.pick:checked::after {
  content: '✓';
  font-size: 0.8rem;
}
.pick:indeterminate::after {
  content: '–';
  font-size: 0.9rem;
}
</style>
