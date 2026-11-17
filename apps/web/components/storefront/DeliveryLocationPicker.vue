<script setup lang="ts">
import { MapPin } from "lucide-vue-next";
import "leaflet/dist/leaflet.css";

// ponytail: Caracas fallback center — swap for tenant-known coords when trivially available.
const DEFAULT_CENTER: [number, number] = [10.4806, -66.9036];
const DEFAULT_ZOOM = 13;

const props = defineProps<{
  modelValue: { lat: number; lng: number } | null;
}>();

const emit = defineEmits<{
  "update:modelValue": [value: { lat: number; lng: number }];
}>();

const mapEl = ref<HTMLDivElement | null>(null);
let map: import("leaflet").Map | null = null;
let marker: import("leaflet").Marker | null = null;

function setPoint(lat: number, lng: number) {
  emit("update:modelValue", { lat, lng });
}

onMounted(async () => {
  const L = await import("leaflet");
  if (!mapEl.value) return;

  const center: [number, number] = props.modelValue
    ? [props.modelValue.lat, props.modelValue.lng]
    : DEFAULT_CENTER;

  map = L.map(mapEl.value).setView(center, DEFAULT_ZOOM);
  L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
    attribution: "&copy; OpenStreetMap contributors",
    maxZoom: 19,
  }).addTo(map);

  const icon = L.divIcon({
    className: "",
    html: '<svg width="32" height="32" viewBox="0 0 24 24" fill="#7C3AED" xmlns="http://www.w3.org/2000/svg"><path d="M12 2C8.13 2 5 5.13 5 9c0 5.25 7 13 7 13s7-7.75 7-13c0-3.87-3.13-7-7-7zm0 9.5A2.5 2.5 0 1 1 12 6.5a2.5 2.5 0 0 1 0 5z"/></svg>',
    iconSize: [32, 32],
    iconAnchor: [16, 32],
  });

  if (props.modelValue) {
    marker = L.marker(center, { icon, draggable: true }).addTo(map);
    marker.on("dragend", () => {
      const pos = marker!.getLatLng();
      setPoint(pos.lat, pos.lng);
    });
  }

  map.on("click", (e: import("leaflet").LeafletMouseEvent) => {
    const { lat, lng } = e.latlng;
    if (marker) {
      marker.setLatLng([lat, lng]);
    } else {
      marker = L.marker([lat, lng], { icon, draggable: true }).addTo(map!);
      marker.on("dragend", () => {
        const pos = marker!.getLatLng();
        setPoint(pos.lat, pos.lng);
      });
    }
    setPoint(lat, lng);
  });
});

onBeforeUnmount(() => {
  map?.remove();
  map = null;
  marker = null;
});
</script>

<template>
  <div>
    <div
      ref="mapEl"
      class="h-64 w-full rounded-lg border border-input overflow-hidden"
    />
    <p class="mt-1.5 text-xs text-muted-foreground flex items-center gap-1">
      <MapPin class="w-3 h-3" />
      {{
        modelValue
          ? `Punto seleccionado: ${modelValue.lat.toFixed(5)}, ${modelValue.lng.toFixed(5)}`
          : "Tocá el mapa para marcar el punto exacto de entrega"
      }}
    </p>
  </div>
</template>
