"use client";

import * as React from "react";
import {
  User as UserIcon,
  ShieldCheck,
  KeyRound,
  Bell,
  Loader2,
  Save,
  Smartphone,
  Mail,
  Phone,
  CheckCircle2,
  XCircle,
  Globe,
  Palette,
} from "lucide-react";
import { toast } from "sonner";
import { useTheme } from "next-themes";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Switch } from "@/components/ui/switch";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Separator } from "@/components/ui/separator";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import { ApiError, authApi, type TwoFactorSetup } from "@/lib/api";
import { useAuthStore } from "@/stores/auth-store";
import { useUIStore, type ProfileTab } from "@/stores/ui-store";
import { initials } from "@/lib/format";
import { ViewHeader } from "./view-header";

// ====== Helpers ======

function errorOf(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof Error) return err.message;
  return "Une erreur est survenue";
}

// ====== Onglet « Mon profil » ======

function ProfileInfosTab() {
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);

  const [fullName, setFullName] = React.useState(user?.fullName ?? "");
  const [phone, setPhone] = React.useState(user?.phone ?? "");
  const [saving, setSaving] = React.useState(false);

  // Re-sync quand le user change (ex: rafraîchissement /me).
  React.useEffect(() => {
    setFullName(user?.fullName ?? "");
    setPhone(user?.phone ?? "");
  }, [user]);

  async function handleSave() {
    setSaving(true);
    try {
      const res = await authApi.updateProfile({
        full_name: fullName.trim(),
        phone: phone.trim() || undefined,
      });
      const updatedUser = res?.user ?? (res as unknown as { user?: typeof user })?.user;
      if (updatedUser) {
        setUser({ ...user, ...updatedUser } as typeof user);
      } else if (user) {
        setUser({ ...user, fullName: fullName.trim(), phone: phone.trim() });
      }
      toast.success("Profil mis à jour", {
        description: "Vos informations personnelles ont été enregistrées.",
      });
    } catch (err) {
      // En mode démo (backend 501), on enregistre localement.
      if (user) {
        setUser({ ...user, fullName: fullName.trim(), phone: phone.trim() });
      }
      toast.error("Enregistrement local", {
        description:
          errorOf(err) +
          " — les modifications sont conservées dans cette session.",
      });
    } finally {
      setSaving(false);
    }
  }

  const name = user?.fullName || "Commerçant";
  const email = user?.email || "—";
  const role = user?.role || "owner";

  return (
    <div className="space-y-5">
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-base">
            <UserIcon className="size-4 text-brand" /> Identité
          </CardTitle>
          <CardDescription>
            Ces informations apparaissent dans votre tableau de bord et sur vos reçus.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-col items-start gap-4 sm:flex-row sm:items-center">
            <Avatar className="size-16">
              {user?.avatar_url ? <AvatarImage src={user.avatar_url} alt={name} /> : null}
              <AvatarFallback className="bg-brand text-brand-foreground text-base font-semibold">
                {initials(name)}
              </AvatarFallback>
            </Avatar>
            <div className="space-y-1">
              <div className="text-base font-semibold">{name}</div>
              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <Badge variant="outline" className="capitalize">{role}</Badge>
                <span className="inline-flex items-center gap-1">
                  <Mail className="size-3" /> {email}
                </span>
                {phone ? (
                  <span className="inline-flex items-center gap-1">
                    <Phone className="size-3" /> {phone}
                  </span>
                ) : null}
              </div>
            </div>
          </div>

          <Separator />

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label htmlFor="full-name">Nom complet</Label>
              <Input
                id="full-name"
                value={fullName}
                onChange={(e) => setFullName(e.target.value)}
                placeholder="Ex : Awa Koné"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="phone">Téléphone</Label>
              <div className="relative">
                <Phone className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  id="phone"
                  value={phone}
                  onChange={(e) => setPhone(e.target.value)}
                  placeholder="+225 07 00 00 00 00"
                  className="pl-9"
                />
              </div>
            </div>
            <div className="space-y-1.5 sm:col-span-2">
              <Label htmlFor="email">Adresse e-mail</Label>
              <div className="relative">
                <Mail className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  id="email"
                  value={email}
                  readOnly
                  disabled
                  className="pl-9 cursor-not-allowed opacity-70"
                />
              </div>
              <p className="text-[11px] text-muted-foreground">
                L&apos;adresse e-mail ne peut pas être modifiée. Contactez le support NOVA en cas de besoin.
              </p>
            </div>
          </div>

          <div className="flex justify-end">
            <Button onClick={handleSave} disabled={saving || !fullName.trim()}>
              {saving ? (
                <>
                  <Loader2 className="size-4 animate-spin" /> Enregistrement…
                </>
              ) : (
                <>
                  <Save className="size-4" /> Enregistrer
                </>
              )}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

// ====== Onglet « Sécurité » ======

function ProfileSecurityTab() {
  const [oldPwd, setOldPwd] = React.useState("");
  const [newPwd, setNewPwd] = React.useState("");
  const [confirmPwd, setConfirmPwd] = React.useState("");
  const [saving, setSaving] = React.useState(false);

  const strength = React.useMemo(() => passwordStrength(newPwd), [newPwd]);

  function reset() {
    setOldPwd("");
    setNewPwd("");
    setConfirmPwd("");
  }

  async function handleSave() {
    if (newPwd.length < 8) {
      toast.error("Mot de passe trop court", {
        description: "Utilisez au moins 8 caractères.",
      });
      return;
    }
    if (newPwd !== confirmPwd) {
      toast.error("Confirmation incorrecte", {
        description: "Le mot de passe et sa confirmation ne correspondent pas.",
      });
      return;
    }
    if (!oldPwd) {
      toast.error("Ancien mot de passe requis", {
        description: "Saisissez votre mot de passe actuel pour valider le changement.",
      });
      return;
    }
    setSaving(true);
    try {
      await authApi.changePassword(oldPwd, newPwd);
      toast.success("Mot de passe modifié", {
        description: "Utilisez votre nouveau mot de passe à la prochaine connexion.",
      });
      reset();
    } catch (err) {
      toast.error("Échec du changement de mot de passe", {
        description: errorOf(err),
      });
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center gap-2 text-base">
          <KeyRound className="size-4 text-brand" /> Changer de mot de passe
        </CardTitle>
        <CardDescription>
          Choisissez un mot de passe robuste (min. 8 caractères, idéal : 12+ avec majuscules, chiffres et symboles).
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-4 sm:grid-cols-3">
          <div className="space-y-1.5">
            <Label htmlFor="old-pwd">Mot de passe actuel</Label>
            <Input
              id="old-pwd"
              type="password"
              value={oldPwd}
              onChange={(e) => setOldPwd(e.target.value)}
              placeholder="••••••••"
              autoComplete="current-password"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="new-pwd">Nouveau mot de passe</Label>
            <Input
              id="new-pwd"
              type="password"
              value={newPwd}
              onChange={(e) => setNewPwd(e.target.value)}
              placeholder="••••••••"
              autoComplete="new-password"
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="confirm-pwd">Confirmer</Label>
            <Input
              id="confirm-pwd"
              type="password"
              value={confirmPwd}
              onChange={(e) => setConfirmPwd(e.target.value)}
              placeholder="••••••••"
              autoComplete="new-password"
            />
          </div>
        </div>

        {newPwd.length > 0 && (
          <div className="space-y-1">
            <div className="flex items-center justify-between text-xs">
              <span className="text-muted-foreground">Robustesse</span>
              <span
                className={cn(
                  "font-medium",
                  strength.color,
                )}
              >
                {strength.label}
              </span>
            </div>
            <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
              <div
                className={cn("h-full rounded-full transition-all", strength.bar)}
                style={{ width: `${strength.score}%` }}
              />
            </div>
          </div>
        )}

        <div className="flex justify-end">
          <Button
            onClick={handleSave}
            disabled={saving || !oldPwd || !newPwd || !confirmPwd}
          >
            {saving ? (
              <>
                <Loader2 className="size-4 animate-spin" /> Modification…
              </>
            ) : (
              <>
                <ShieldCheck className="size-4" /> Mettre à jour
              </>
            )}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

// ====== Onglet « 2FA » ======

function Profile2FATab() {
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);

  const [enabled, setEnabled] = React.useState<boolean>(!!user?.two_factor_enabled);
  React.useEffect(() => {
    setEnabled(!!user?.two_factor_enabled);
  }, [user]);

  const [setup, setSetup] = React.useState<TwoFactorSetup | null>(null);
  const [code, setCode] = React.useState("");
  const [loading, setLoading] = React.useState(false);
  const [disabling, setDisabling] = React.useState(false);
  const [disableCode, setDisableCode] = React.useState("");

  async function handleSetup() {
    setLoading(true);
    try {
      const res = await authApi.setup2FA();
      setSetup(res);
      toast.info("Configurez votre application 2FA", {
        description: "Scannez le QR code avec Google Authenticator, Authy ou 1Password.",
      });
    } catch (err) {
      toast.error("Impossible d&apos;initialiser la 2FA", {
        description: errorOf(err),
      });
    } finally {
      setLoading(false);
    }
  }

  async function handleConfirm() {
    if (code.length < 6) {
      toast.error("Code invalide", {
        description: "Saisissez les 6 chiffres affichés par votre application.",
      });
      return;
    }
    setLoading(true);
    try {
      await authApi.confirm2FA(code);
      setEnabled(true);
      setSetup(null);
      setCode("");
      if (user) setUser({ ...user, two_factor_enabled: true });
      toast.success("2FA activée", {
        description: "Votre compte est désormais protégé par double authentification.",
      });
    } catch (err) {
      toast.error("Code incorrect", { description: errorOf(err) });
    } finally {
      setLoading(false);
    }
  }

  async function handleDisable() {
    if (disableCode.length < 6) {
      toast.error("Code requis", {
        description: "Saisissez un code 2FA valide pour désactiver.",
      });
      return;
    }
    setDisabling(true);
    try {
      await authApi.disable2FA(disableCode);
      setEnabled(false);
      setDisableCode("");
      if (user) setUser({ ...user, two_factor_enabled: false });
      toast.success("2FA désactivée", {
        description: "Vous pouvez la réactiver à tout moment.",
      });
    } catch (err) {
      toast.error("Code incorrect", { description: errorOf(err) });
    } finally {
      setDisabling(false);
    }
  }

  return (
    <div className="space-y-5">
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-base">
            <ShieldCheck className="size-4 text-brand" /> Authentification à deux facteurs
          </CardTitle>
          <CardDescription>
            Renforcez la sécurité de votre compte avec une seconde vérification (application TOTP : Google Authenticator, Authy…).
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between rounded-lg border p-3">
            <div className="flex items-center gap-3">
              <span
                className={cn(
                  "flex size-9 items-center justify-center rounded-md",
                  enabled ? "bg-emerald-500/10 text-emerald-600" : "bg-muted text-muted-foreground",
                )}
              >
                {enabled ? <CheckCircle2 className="size-5" /> : <XCircle className="size-5" />}
              </span>
              <div>
                <div className="text-sm font-medium">
                  État : {enabled ? "Activée" : "Désactivée"}
                </div>
                <div className="text-xs text-muted-foreground">
                  {enabled
                    ? "Votre compte exige un code 2FA à la connexion."
                    : "Activez la 2FA pour protéger votre compte contre le piratage."}
                </div>
              </div>
            </div>
            {enabled ? (
              <Badge className="bg-emerald-500/10 text-emerald-700 dark:text-emerald-300">
                Sécurisé
              </Badge>
            ) : (
              <Badge variant="outline">Non protégé</Badge>
            )}
          </div>

          {!enabled && !setup && (
            <div className="flex justify-end">
              <Button onClick={handleSetup} disabled={loading}>
                {loading ? (
                  <>
                    <Loader2 className="size-4 animate-spin" /> Initialisation…
                  </>
                ) : (
                  <>
                    <ShieldCheck className="size-4" /> Activer la 2FA
                  </>
                )}
              </Button>
            </div>
          )}

          {setup && (
            <div className="space-y-4 rounded-lg border bg-muted/30 p-4">
              <div className="grid gap-4 sm:grid-cols-[auto_1fr] sm:items-center">
                <div className="rounded-md bg-white p-2">
                  <img
                    src={setup.qr_data_uri}
                    alt="QR code 2FA"
                    className="size-40"
                  />
                </div>
                <div className="space-y-2 text-sm">
                  <div className="font-medium">Scannez ce QR code</div>
                  <p className="text-xs text-muted-foreground">
                    Ouvrez votre application d&apos;authentification (Google Authenticator, Authy, 1Password…)
                    et ajoutez une nouvelle entrée en scannant ce QR code.
                  </p>
                  <div className="space-y-1">
                    <span className="text-xs font-medium text-muted-foreground">
                      Ou saisissez manuellement ce secret :
                    </span>
                    <code className="block rounded bg-background px-2 py-1 text-xs break-all">
                      {setup.secret}
                    </code>
                  </div>
                </div>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="code-2fa">Code à 6 chiffres</Label>
                <Input
                  id="code-2fa"
                  value={code}
                  onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
                  inputMode="numeric"
                  placeholder="123456"
                  className="max-w-[200px] text-lg tracking-[0.3em]"
                />
              </div>
              <div className="flex justify-end gap-2">
                <Button
                  variant="outline"
                  onClick={() => {
                    setSetup(null);
                    setCode("");
                  }}
                >
                  Annuler
                </Button>
                <Button onClick={handleConfirm} disabled={loading || code.length < 6}>
                  {loading ? (
                    <>
                      <Loader2 className="size-4 animate-spin" /> Validation…
                    </>
                  ) : (
                    <>
                      <CheckCircle2 className="size-4" /> Confirmer
                    </>
                  )}
                </Button>
              </div>
            </div>
          )}

          {enabled && (
            <div className="space-y-3 rounded-lg border border-amber-accent/30 bg-amber-accent/5 p-4">
              <div className="space-y-1">
                <Label htmlFor="disable-2fa">Désactiver la 2FA</Label>
                <p className="text-xs text-muted-foreground">
                  Saisissez un code 2FA valide pour confirmer la désactivation. Votre compte sera moins protégé.
                </p>
              </div>
              <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
                <div className="space-y-1.5 sm:flex-1">
                  <Input
                    id="disable-2fa"
                    value={disableCode}
                    onChange={(e) =>
                      setDisableCode(e.target.value.replace(/\D/g, "").slice(0, 6))
                    }
                    inputMode="numeric"
                    placeholder="Code 2FA"
                    className="text-lg tracking-[0.3em]"
                  />
                </div>
                <Button
                  variant="destructive"
                  onClick={handleDisable}
                  disabled={disabling || disableCode.length < 6}
                >
                  {disabling ? (
                    <>
                      <Loader2 className="size-4 animate-spin" /> Désactivation…
                    </>
                  ) : (
                    <>
                      <XCircle className="size-4" /> Désactiver
                    </>
                  )}
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

// ====== Onglet « Paramètres » ======

interface NotificationPrefs {
  ordersEmail: boolean;
  ordersWhatsapp: boolean;
  stockAlerts: boolean;
  dailyDigest: boolean;
  marketing: boolean;
}

const DEFAULT_NOTIFS: NotificationPrefs = {
  ordersEmail: true,
  ordersWhatsapp: true,
  stockAlerts: true,
  dailyDigest: false,
  marketing: false,
};

function ProfileSettingsTab() {
  const { theme, setTheme } = useTheme();
  const [mounted, setMounted] = React.useState(false);
  React.useEffect(() => setMounted(true), []);

  const [notifs, setNotifs] = React.useState<NotificationPrefs>(() => {
    if (typeof window === "undefined") return DEFAULT_NOTIFS;
    try {
      const raw = window.localStorage.getItem("nova-notif-prefs");
      if (raw) return { ...DEFAULT_NOTIFS, ...(JSON.parse(raw) as Partial<NotificationPrefs>) };
    } catch {
      // ignore
    }
    return DEFAULT_NOTIFS;
  });

  function update<K extends keyof NotificationPrefs>(key: K, value: boolean) {
    const next = { ...notifs, [key]: value };
    setNotifs(next);
    try {
      window.localStorage.setItem("nova-notif-prefs", JSON.stringify(next));
    } catch {
      // ignore
    }
    toast.success("Préférence enregistrée");
  }

  return (
    <div className="space-y-5">
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-base">
            <Bell className="size-4 text-brand" /> Notifications
          </CardTitle>
          <CardDescription>
            Choisissez les alertes que vous souhaitez recevoir sur vos canaux.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-1">
          <NotifRow
            icon={<Mail className="size-4" />}
            title="Nouvelles commandes (e-mail)"
            description="Recevoir un e-mail à chaque nouvelle commande."
            checked={notifs.ordersEmail}
            onCheck={(v) => update("ordersEmail", v)}
          />
          <NotifRow
            icon={<Smartphone className="size-4" />}
            title="Nouvelles commandes (WhatsApp)"
            description="Recevoir une notification WhatsApp à chaque commande."
            checked={notifs.ordersWhatsapp}
            onCheck={(v) => update("ordersWhatsapp", v)}
          />
          <NotifRow
            icon={<Bell className="size-4" />}
            title="Alertes de stock bas"
            description="Être prévenu quand un produit atteint son seuil d'alerte."
            checked={notifs.stockAlerts}
            onCheck={(v) => update("stockAlerts", v)}
          />
          <NotifRow
            icon={<Mail className="size-4" />}
            title="Récap quotidien"
            description="Recevoir un résumé des ventes chaque matin."
            checked={notifs.dailyDigest}
            onCheck={(v) => update("dailyDigest", v)}
          />
          <NotifRow
            icon={<Bell className="size-4" />}
            title="Conseils & nouveautés NOVA"
            description="Nouveautés produit, astuces et offres promotionnelles."
            checked={notifs.marketing}
            onCheck={(v) => update("marketing", v)}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-base">
            <Palette className="size-4 text-brand" /> Apparence
          </CardTitle>
          <CardDescription>
            Personnalisez le rendu visuel de votre tableau de bord.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between rounded-lg border p-3">
            <div className="flex items-center gap-3">
              <span className="flex size-9 items-center justify-center rounded-md bg-muted text-muted-foreground">
                <Palette className="size-5" />
              </span>
              <div>
                <div className="text-sm font-medium">Thème</div>
                <div className="text-xs text-muted-foreground">
                  Choisissez entre le mode clair et sombre.
                </div>
              </div>
            </div>
            <Select
              value={mounted ? (theme ?? "system") : "system"}
              onValueChange={(v) => setTheme(v)}
            >
              <SelectTrigger className="w-[140px]" size="sm">
                <SelectValue placeholder="Thème" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="light">Clair</SelectItem>
                <SelectItem value="dark">Sombre</SelectItem>
                <SelectItem value="system">Système</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="flex items-center justify-between rounded-lg border p-3 opacity-70">
            <div className="flex items-center gap-3">
              <span className="flex size-9 items-center justify-center rounded-md bg-muted text-muted-foreground">
                <Globe className="size-5" />
              </span>
              <div>
                <div className="text-sm font-medium">Langue</div>
                <div className="text-xs text-muted-foreground">
                  NOVA est actuellement disponible en français uniquement.
                </div>
              </div>
            </div>
            <Select value="fr" disabled>
              <SelectTrigger className="w-[140px]" size="sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="fr">Français</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

function NotifRow({
  icon,
  title,
  description,
  checked,
  onCheck,
}: {
  icon: React.ReactNode;
  title: string;
  description: string;
  checked: boolean;
  onCheck: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-lg border-b border-dashed py-3 last:border-0 last:pb-0">
      <div className="flex items-start gap-3">
        <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
          {icon}
        </span>
        <div>
          <div className="text-sm font-medium">{title}</div>
          <div className="text-xs text-muted-foreground">{description}</div>
        </div>
      </div>
      <Switch checked={checked} onCheckedChange={onCheck} />
    </div>
  );
}

// ====== Force du mot de passe ======

function passwordStrength(pwd: string): {
  score: number;
  label: string;
  color: string;
  bar: string;
} {
  let score = 0;
  if (pwd.length >= 8) score += 25;
  if (pwd.length >= 12) score += 15;
  if (/[A-Z]/.test(pwd)) score += 15;
  if (/[a-z]/.test(pwd)) score += 15;
  if (/[0-9]/.test(pwd)) score += 15;
  if (/[^A-Za-z0-9]/.test(pwd)) score += 15;
  score = Math.min(100, score);
  if (score < 35) return { score, label: "Faible", color: "text-red-600 dark:text-red-400", bar: "bg-red-500" };
  if (score < 65) return { score, label: "Moyen", color: "text-amber-600 dark:text-amber-400", bar: "bg-amber-500" };
  if (score < 85) return { score, label: "Bon", color: "text-sky-600 dark:text-sky-400", bar: "bg-sky-500" };
  return { score, label: "Fort", color: "text-emerald-600 dark:text-emerald-400", bar: "bg-emerald-500" };
}

// ====== Vue principale ======

export function ProfileView() {
  const profileTab = useUIStore((s) => s.profileTab);
  const setProfileTab = useUIStore((s) => s.setProfileTab);

  const tab: ProfileTab = profileTab;

  return (
    <div className="space-y-5">
      <ViewHeader
        title="Profil & paramètres"
        description="Gérez vos informations personnelles, la sécurité de votre compte et vos préférences."
      />

      <Tabs value={tab} onValueChange={(v) => setProfileTab(v as ProfileTab)} className="space-y-5">
        <TabsList className="flex w-full max-w-fit flex-wrap">
          <TabsTrigger value="infos" className="gap-1.5">
            <UserIcon className="size-3.5" /> Mon profil
          </TabsTrigger>
          <TabsTrigger value="securite" className="gap-1.5">
            <KeyRound className="size-3.5" /> Sécurité
          </TabsTrigger>
          <TabsTrigger value="2fa" className="gap-1.5">
            <ShieldCheck className="size-3.5" /> 2FA
          </TabsTrigger>
          <TabsTrigger value="parametres" className="gap-1.5">
            <Bell className="size-3.5" /> Paramètres
          </TabsTrigger>
        </TabsList>

        <TabsContent value="infos">
          <ProfileInfosTab />
        </TabsContent>
        <TabsContent value="securite">
          <ProfileSecurityTab />
        </TabsContent>
        <TabsContent value="2fa">
          <Profile2FATab />
        </TabsContent>
        <TabsContent value="parametres">
          <ProfileSettingsTab />
        </TabsContent>
      </Tabs>
    </div>
  );
}
