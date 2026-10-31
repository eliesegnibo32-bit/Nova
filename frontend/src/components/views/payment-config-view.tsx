"use client";

import * as React from "react";
import {
  Wallet,
  Banknote,
  Clock,
  Save,
  Loader2,
  Truck,
  HandCoins,
  CheckCircle2,
  AlertCircle,
  Link as LinkIcon,
  Phone,
} from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";
import {
  paymentConfigApi,
  type PaymentConfig,
  type PaymentConfigInput,
  type PaymentMethod,
  type PaymentMode,
  ApiError,
} from "@/lib/api";
import { formatFCFA } from "@/lib/format";
import { ViewHeader } from "./view-header";

// ====== Helpers & constantes ======

const MODES: {
  key: PaymentMode;
  label: string;
  description: string;
  icon: React.ComponentType<{ className?: string }>;
}[] = [
  {
    key: "paiement_livraison",
    label: "Paiement à la livraison",
    description: "Le client paie en espèces à la réception de sa commande.",
    icon: Truck,
  },
  {
    key: "paiement_avance",
    label: "Paiement avec avance",
    description:
      "Le client paie une avance pour confirmer, le solde à la livraison.",
    icon: HandCoins,
  },
  {
    key: "paiement_integral",
    label: "Paiement intégral",
    description: "Le client paie la totalité avant la préparation de la commande.",
    icon: Banknote,
  },
];

const METHODS: {
  key: PaymentMethod;
  label: string;
  color: string;
  needsLink?: boolean;
}[] = [
  { key: "wave", label: "Wave", color: "bg-sky-500/10 text-sky-700 dark:text-sky-300", needsLink: true },
  { key: "moov", label: "Moov Money", color: "bg-blue-700/10 text-blue-700 dark:text-blue-300" },
  { key: "orange", label: "Orange Money", color: "bg-orange-500/10 text-orange-700 dark:text-orange-300" },
  { key: "mtn", label: "MTN MoMo", color: "bg-yellow-400/10 text-yellow-700 dark:text-yellow-300" },
];

const DEFAULT_CONFIG: PaymentConfig = {
  shop_id: "",
  mode: "paiement_livraison",
  delay_minutes: 120,
  advance_amount: null,
  wave_link: null,
  wave_number: null,
  moov_number: null,
  orange_number: null,
  mtn_number: null,
  active_methods: [],
};

const FALLBACK_CONFIG: PaymentConfig = {
  shop_id: "",
  mode: "paiement_avance",
  delay_minutes: 120,
  advance_amount: 2000,
  wave_link: "https://pay.wave.com/m/yBoutiqueDemo",
  wave_number: "+225 07 00 00 00 01",
  moov_number: "+225 07 00 00 00 02",
  orange_number: "+225 07 00 00 00 03",
  mtn_number: "+225 05 00 00 00 04",
  active_methods: ["wave", "moov", "orange", "mtn"],
};

function delayLabel(minutes: number): string {
  if (minutes <= 0) return "Immédiat";
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  if (h === 0) return `${m} min`;
  if (m === 0) return `${h} h`;
  return `${h} h ${m.toString().padStart(2, "0")}`;
}

// ====== Vue principale ======

export function PaymentConfigView() {
  const [config, setConfig] = React.useState<PaymentConfig>(DEFAULT_CONFIG);
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);
  const [dirty, setDirty] = React.useState(false);

  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      const data = await paymentConfigApi.get();
      setConfig({ ...DEFAULT_CONFIG, ...data });
    } catch (err) {
      // Silent fallback — don't show error toast, just use default config
      // The user can still configure and save; the backend will create the config on save.
      setConfig(DEFAULT_CONFIG);
    } finally {
      setLoading(false);
      setDirty(false);
    }
  }, []);

  React.useEffect(() => {
    load();
  }, [load]);

  function update<K extends keyof PaymentConfig>(key: K, value: PaymentConfig[K]) {
    setConfig((prev) => ({ ...prev, [key]: value }));
    setDirty(true);
  }

  function toggleMethod(m: PaymentMethod) {
    setConfig((prev) => {
      const active = prev.active_methods.includes(m)
        ? prev.active_methods.filter((x) => x !== m)
        : [...prev.active_methods, m];
      return { ...prev, active_methods: active };
    });
    setDirty(true);
  }

  async function handleSave() {
    if (config.mode === "paiement_avance" && !config.advance_amount) {
      toast.error("Avance obligatoire", {
        description: "Indiquez le montant de l'avance à encaisser.",
      });
      return;
    }
    if (config.active_methods.length === 0 && config.mode !== "paiement_livraison") {
      toast.error("Aucun moyen de paiement", {
        description: "Activez au moins un moyen de paiement mobile.",
      });
      return;
    }
    setSaving(true);
    try {
      const payload: PaymentConfigInput = {
        mode: config.mode,
        delay_minutes: config.delay_minutes,
        advance_amount: config.mode === "paiement_avance" ? config.advance_amount : null,
        wave_link: config.wave_link || null,
        wave_number: config.wave_number || null,
        moov_number: config.moov_number || null,
        orange_number: config.orange_number || null,
        mtn_number: config.mtn_number || null,
        active_methods: config.active_methods,
      };
      const updated = await paymentConfigApi.update(payload);
      setConfig({ ...DEFAULT_CONFIG, ...updated });
      setDirty(false);
      toast.success("Configuration enregistrée", {
        description: "Les nouveaux paramètres de paiement sont actifs.",
      });
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : "Échec de l'enregistrement";
      toast.error("Échec de l'enregistrement", { description: msg });
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Paiement"
        description="Configurez les modalités de paiement acceptées par votre boutique (livraison, avance, intégral)."
        actions={
          <Button onClick={handleSave} disabled={saving || !dirty || loading}>
            {saving ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Save className="size-4" />
            )}
            Enregistrer
          </Button>
        }
      />

      {loading ? (
        <div className="space-y-4">
          <Skeleton className="h-40 w-full" />
          <Skeleton className="h-64 w-full" />
        </div>
      ) : (
        <div className="grid gap-5 lg:grid-cols-3">
          {/* Colonne principale : formulaire */}
          <div className="space-y-5 lg:col-span-2">
            {/* Mode de paiement */}
            <Card>
              <CardHeader className="pb-3">
                <CardTitle className="flex items-center gap-2 text-base">
                  <Wallet className="size-4 text-brand" /> Mode de paiement
                </CardTitle>
                <CardDescription>
                  Choisissez la stratégie d'encaissement par défaut pour les commandes.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                <RadioGroup
                  value={config.mode}
                  onValueChange={(v) => update("mode", v as PaymentMode)}
                  className="gap-2.5"
                >
                  {MODES.map((m) => {
                    const active = config.mode === m.key;
                    return (
                      <label
                        key={m.key}
                        htmlFor={`mode-${m.key}`}
                        className={cn(
                          "flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors",
                          active
                            ? "border-brand bg-brand/5 ring-1 ring-brand/30"
                            : "hover:bg-accent",
                        )}
                      >
                        <RadioGroupItem
                          id={`mode-${m.key}`}
                          value={m.key}
                          className="mt-1"
                        />
                        <span
                          className={cn(
                            "flex size-8 shrink-0 items-center justify-center rounded-md",
                            active
                              ? "bg-brand text-brand-foreground"
                              : "bg-muted text-muted-foreground",
                          )}
                        >
                          <m.icon className="size-4" />
                        </span>
                        <div className="min-w-0 flex-1">
                          <div className="text-sm font-medium">{m.label}</div>
                          <div className="text-xs text-muted-foreground">
                            {m.description}
                          </div>
                        </div>
                      </label>
                    );
                  })}
                </RadioGroup>

                {config.mode === "paiement_avance" && (
                  <div className="space-y-1.5 rounded-lg border border-amber-accent/30 bg-amber-accent/5 p-3">
                    <Label htmlFor="advance-amount" className="text-xs font-semibold text-amber-accent">
                      Montant de l'avance (FCFA)
                    </Label>
                    <Input
                      id="advance-amount"
                      type="number"
                      min="0"
                      step="100"
                      value={config.advance_amount ?? ""}
                      onChange={(e) =>
                        update(
                          "advance_amount",
                          e.target.value === "" ? null : Number(e.target.value),
                        )
                      }
                      placeholder="Ex : 2000"
                      className="bg-background"
                    />
                    <p className="text-[11px] text-muted-foreground">
                      Le client devra payer cette avance pour confirmer sa commande.
                      Le solde de {formatFCFA(0).replace("0\u00A0FCFA", "…")} sera réglé à la livraison.
                    </p>
                  </div>
                )}
              </CardContent>
            </Card>

            {/* Moyens de paiement mobile */}
            <Card>
              <CardHeader className="pb-3">
                <CardTitle className="flex items-center gap-2 text-base">
                  <Banknote className="size-4 text-brand" /> Moyens de paiement mobile
                </CardTitle>
                <CardDescription>
                  Activez les opérateurs Mobile Money acceptés par votre boutique.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                {METHODS.map((m) => {
                  const active = config.active_methods.includes(m.key);
                  return (
                    <div
                      key={m.key}
                      className={cn(
                        "rounded-lg border p-3 transition-colors",
                        active ? "border-brand/40 bg-brand/5" : "opacity-70",
                      )}
                    >
                      <div className="flex items-center justify-between gap-3">
                        <div className="flex items-center gap-2.5">
                          <Checkbox
                            id={`m-${m.key}`}
                            checked={active}
                            onCheckedChange={() => toggleMethod(m.key)}
                          />
                          <Label
                            htmlFor={`m-${m.key}`}
                            className="cursor-pointer text-sm font-medium"
                          >
                            {m.label}
                          </Label>
                          <Badge variant="outline" className={cn("text-[10px]", m.color)}>
                            {m.label}
                          </Badge>
                        </div>
                      </div>
                      {active && (
                        <div className="mt-3 grid gap-2 sm:grid-cols-2">
                          {m.needsLink && (
                            <div className="space-y-1">
                              <Label htmlFor={`link-${m.key}`} className="text-[11px] text-muted-foreground">
                                <LinkIcon className="mr-1 inline size-3" />
                                Lien de paiement
                              </Label>
                              <Input
                                id={`link-${m.key}`}
                                value={config[`${m.key}_link` as keyof PaymentConfig] as string ?? ""}
                                onChange={(e) =>
                                  update(
                                    `${m.key}_link` as keyof PaymentConfig,
                                    e.target.value as never,
                                  )
                                }
                                placeholder="https://pay.wave.com/…"
                                className="h-8 text-xs"
                              />
                            </div>
                          )}
                          <div className="space-y-1">
                            <Label htmlFor={`num-${m.key}`} className="text-[11px] text-muted-foreground">
                              <Phone className="mr-1 inline size-3" />
                              Numéro {m.label}
                            </Label>
                            <Input
                              id={`num-${m.key}`}
                              value={
                                (config[`${m.key}_number` as keyof PaymentConfig] as string) ??
                                ""
                              }
                              onChange={(e) =>
                                update(
                                  `${m.key}_number` as keyof PaymentConfig,
                                  e.target.value as never,
                                )
                              }
                              placeholder="+225 …"
                              className="h-8 text-xs"
                            />
                          </div>
                        </div>
                      )}
                    </div>
                  );
                })}
              </CardContent>
            </Card>
          </div>

          {/* Colonne latérale : résumé + délai */}
          <div className="space-y-5">
            <Card>
              <CardHeader className="pb-3">
                <CardTitle className="flex items-center gap-2 text-base">
                  <Clock className="size-4 text-brand" /> Délai de préparation
                </CardTitle>
                <CardDescription>
                  Délai affiché au client avant enlèvement / livraison.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                <div className="space-y-1.5">
                  <Label htmlFor="delay">Délai (minutes)</Label>
                  <Input
                    id="delay"
                    type="number"
                    min="0"
                    step="5"
                    value={config.delay_minutes}
                    onChange={(e) =>
                      update("delay_minutes", Math.max(0, Number(e.target.value) || 0))
                    }
                  />
                  <p className="text-[11px] text-muted-foreground">
                    Valeur par défaut : 120 min (2 h).{" "}
                    <strong className="text-foreground">
                      Aperçu : {delayLabel(config.delay_minutes)}
                    </strong>
                  </p>
                </div>
                <div className="flex flex-wrap gap-1.5">
                  {[30, 60, 90, 120, 180, 240].map((m) => (
                    <Button
                      key={m}
                      type="button"
                      variant={config.delay_minutes === m ? "default" : "outline"}
                      size="sm"
                      className="h-7 text-xs"
                      onClick={() => update("delay_minutes", m)}
                    >
                      {delayLabel(m)}
                    </Button>
                  ))}
                </div>
              </CardContent>
            </Card>

            <Card className="overflow-hidden border-brand/30">
              <CardHeader className="border-b bg-brand/5 pb-3">
                <CardTitle className="text-sm font-semibold">Résumé</CardTitle>
                <CardDescription className="text-xs">
                  Configuration active de votre boutique.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-2.5 py-4 text-sm">
                <div className="flex items-start justify-between gap-2">
                  <span className="text-muted-foreground">Mode</span>
                  <span className="text-right font-medium">
                    {MODES.find((m) => m.key === config.mode)?.label ?? "—"}
                  </span>
                </div>
                {config.mode === "paiement_avance" && (
                  <div className="flex items-start justify-between gap-2">
                    <span className="text-muted-foreground">Avance</span>
                    <span className="text-right font-medium tabular-nums">
                      {config.advance_amount ? formatFCFA(config.advance_amount) : "—"}
                    </span>
                  </div>
                )}
                <div className="flex items-start justify-between gap-2">
                  <span className="text-muted-foreground">Délai</span>
                  <span className="text-right font-medium">
                    {delayLabel(config.delay_minutes)}
                  </span>
                </div>
                <Separator className="my-1" />
                <div className="flex items-start justify-between gap-2">
                  <span className="text-muted-foreground">Moyens actifs</span>
                  <div className="flex flex-wrap justify-end gap-1">
                    {config.active_methods.length === 0 ? (
                      <span className="text-xs text-muted-foreground">Aucun</span>
                    ) : (
                      config.active_methods.map((m) => (
                        <Badge key={m} variant="secondary" className="text-[10px]">
                          {METHODS.find((x) => x.key === m)?.label ?? m}
                        </Badge>
                      ))
                    )}
                  </div>
                </div>
                {dirty ? (
                  <div className="mt-2 flex items-start gap-2 rounded-md border border-amber-accent/30 bg-amber-accent/10 p-2 text-[11px] text-amber-accent">
                    <AlertCircle className="mt-0.5 size-3.5 shrink-0" />
                    <span>
                      Modifications non enregistrées — cliquez sur « Enregistrer » en haut à droite.
                    </span>
                  </div>
                ) : (
                  <div className="mt-2 flex items-start gap-2 rounded-md border border-emerald-500/30 bg-emerald-500/10 p-2 text-[11px] text-emerald-700 dark:text-emerald-400">
                    <CheckCircle2 className="mt-0.5 size-3.5 shrink-0" />
                    <span>Configuration synchronisée avec le backend.</span>
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        </div>
      )}
    </div>
  );
}
