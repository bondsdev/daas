<script setup lang="ts">
import { Check, History as HistoryIcon, Loader2 } from "lucide-vue-next";

export interface HistoryDelivery {
  id: string;
  customer_name: string;
  rider_fee: number | null;
  rider_delivered_at: string | null;
  rider_paid_at: string | null;
}

const props = defineProps<{
  deliveries: HistoryDelivery[];
  earningsThisWeek: number;
  deliveriesThisWeek: number;
  pendingPayout: number;
  loading: boolean;
  loadError: string;
}>();

// ponytail: filter chips are decorative-only for now, no real filtering logic.
const activeFilter = ref<"all" | "week" | "month" | "unpaid">("all");
const filterChips = [
  { key: "all" as const, label: "Todas" },
  { key: "week" as const, label: "Esta semana" },
  { key: "month" as const, label: "Este mes" },
  { key: "unpaid" as const, label: "Sin liquidar" },
];

function fmtCurrency(n: number) {
  return new Intl.NumberFormat("es-VE", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(n);
}

function fmtTime(iso: string) {
  return new Date(iso).toLocaleTimeString("es-VE", {
    hour: "numeric",
    minute: "2-digit",
  });
}

function dayLabel(iso: string) {
  const date = new Date(iso);
  const today = new Date();
  const yesterday = new Date();
  yesterday.setDate(today.getDate() - 1);
  const sameDay = (a: Date, b: Date) =>
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate();
  if (sameDay(date, today)) return "Hoy";
  if (sameDay(date, yesterday)) return "Ayer";
  return date.toLocaleDateString("es-VE", { day: "numeric", month: "long" });
}

const groups = computed(() => {
  const byLabel = new Map<string, HistoryDelivery[]>();
  for (const d of props.deliveries) {
    if (!d.rider_delivered_at) continue;
    const label = dayLabel(d.rider_delivered_at);
    if (!byLabel.has(label)) byLabel.set(label, []);
    byLabel.get(label)!.push(d);
  }
  return [...byLabel.entries()].map(([label, items]) => ({ label, items }));
});
</script>

<template>
  <MobileScreen
    title="Historial"
    subtitle="Tus entregas completadas"
    variant="brand"
  >
    <div class="flex gap-2.5 px-4 pt-3.5">
      <div class="flex-1 rounded-xl border-0 bg-accent px-3.5 py-3">
        <p class="text-[11px] font-semibold text-accent-foreground/70">
          Esta semana
        </p>
        <p class="mt-0.5 font-mono text-lg font-bold text-accent-foreground">
          {{ fmtCurrency(earningsThisWeek) }}
        </p>
      </div>
      <div class="flex-1 rounded-xl border border-border px-3.5 py-3">
        <p class="text-[11px] font-semibold text-muted-foreground">Entregas</p>
        <p class="mt-0.5 font-mono text-lg font-bold text-foreground">
          {{ deliveriesThisWeek }}
        </p>
      </div>
      <div class="flex-1 rounded-xl border border-border px-3.5 py-3">
        <p class="text-[11px] font-semibold text-muted-foreground">
          Por liquidar
        </p>
        <p class="mt-0.5 font-mono text-lg font-bold text-foreground">
          {{ fmtCurrency(pendingPayout) }}
        </p>
      </div>
    </div>

    <div class="flex gap-2 overflow-x-auto px-4 pb-2 pt-3">
      <button
        v-for="chip in filterChips"
        :key="chip.key"
        type="button"
        class="no-min-tap shrink-0 rounded-full border px-3.5 py-1.5 text-[13px] font-semibold"
        :class="
          activeFilter === chip.key
            ? 'border-transparent bg-accent text-accent-foreground'
            : 'border-input bg-background text-foreground'
        "
        @click="activeFilter = chip.key"
      >
        {{ chip.label }}
      </button>
    </div>

    <div
      v-if="loadError"
      class="mx-4 mt-3 rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive"
    >
      {{ loadError }}
    </div>

    <div
      v-else-if="loading && !deliveries.length"
      class="flex justify-center py-16"
    >
      <Loader2 class="size-6 animate-spin text-muted-foreground" />
    </div>

    <div v-else-if="!deliveries.length" class="px-6 py-20 text-center">
      <HistoryIcon class="mx-auto mb-4 size-12 text-muted-foreground/20" />
      <p class="font-medium text-muted-foreground">
        Todavía no tenés entregas completadas
      </p>
    </div>

    <div v-else class="px-4 pt-2">
      <div v-for="group in groups" :key="group.label" class="mb-1">
        <p
          class="my-2.5 text-xs font-bold uppercase tracking-wide text-muted-foreground"
        >
          {{ group.label }}
        </p>

        <article
          v-for="item in group.items"
          :key="item.id"
          class="mb-2.5 flex items-center gap-3 rounded-xl border border-border bg-white px-3.5 py-3"
        >
          <div
            class="flex size-[38px] shrink-0 items-center justify-center rounded-[10px]"
            :class="
              item.rider_paid_at
                ? 'bg-emerald-500/10 text-emerald-700'
                : 'bg-orange-400/15 text-orange-500'
            "
          >
            <Check class="size-[18px]" />
          </div>
          <div class="min-w-0 flex-1">
            <div class="flex items-baseline justify-between gap-2">
              <span class="truncate text-[13.5px] font-bold">{{
                item.customer_name
              }}</span>
              <span class="shrink-0 text-[11px] text-muted-foreground">
                {{
                  item.rider_delivered_at
                    ? fmtTime(item.rider_delivered_at)
                    : ""
                }}
              </span>
            </div>
            <div class="mt-0.5 flex items-center gap-1.5">
              <span class="font-mono text-xs font-bold text-primary">
                + {{ fmtCurrency(item.rider_fee ?? 0) }}
              </span>
              <span
                class="text-[10.5px]"
                :class="
                  item.rider_paid_at
                    ? 'text-muted-foreground'
                    : 'font-semibold text-orange-500'
                "
              >
                · {{ item.rider_paid_at ? "liquidado" : "por liquidar" }}
              </span>
            </div>
          </div>
        </article>
      </div>

      <p class="py-10 text-center text-[13px] text-muted-foreground">
        No hay más entregas antes de esta fecha.
      </p>
    </div>
  </MobileScreen>
</template>
