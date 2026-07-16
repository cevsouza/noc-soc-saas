'use client';

import React, { useState } from 'react';
import { useRouter } from 'next/navigation';
import { AlertTriangle, CheckCircle2, Eye, EyeOff, RefreshCw } from 'lucide-react';
import { useAuth } from '@/lib/auth-context';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';

type AuthView = 'login' | 'register';
type AuthStatus = { status: 'idle' | 'loading' | 'success' | 'error'; message?: string };

export default function LoginPage() {
  const router = useRouter();
  const { login, register } = useAuth();

  const [authView, setAuthView] = useState<AuthView>('login');
  const [authStatus, setAuthStatus] = useState<AuthStatus>({ status: 'idle' });

  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [name, setName] = useState('');

  const [showLoginPassword, setShowLoginPassword] = useState(false);
  const [showSignupPassword, setShowSignupPassword] = useState(false);
  const [showSignupConfirmPassword, setShowSignupConfirmPassword] = useState(false);
  const [signupEmailError, setSignupEmailError] = useState('');
  const [signupPasswordError, setSignupPasswordError] = useState('');

  const verifiedBanner = typeof window !== 'undefined' && window.location.search.includes('verified=true');

  const handleEmailChange = (value: string) => {
    setEmail(value);
    if (authView === 'register') {
      const regex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
      setSignupEmailError(value && !regex.test(value) ? 'Formato de e-mail inválido' : '');
    }
  };

  const handlePasswordChange = (value: string) => {
    setPassword(value);
    if (authView === 'register') {
      setSignupPasswordError(value && value.length < 6 ? 'A senha deve ter pelo menos 6 caracteres' : '');
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    if (authView === 'login') {
      setAuthStatus({ status: 'loading' });
      const result = await login(email, password);
      if (result.ok) {
        setAuthStatus({ status: 'success' });
        // Land on the user's preferred console (B9): /noc or /soc, else the unified cockpit.
        const dest = result.defaultConsole === 'noc' ? '/noc' : result.defaultConsole === 'soc' ? '/soc' : '/';
        router.push(dest);
      } else {
        setAuthStatus({ status: 'error', message: result.message });
      }
      return;
    }

    // Register flow — same client-side validation order as the original implementation.
    const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
    if (!emailRegex.test(email)) {
      setAuthStatus({ status: 'error', message: 'Por favor, informe um endereço de e-mail válido.' });
      return;
    }
    if (password.length < 6) {
      setAuthStatus({ status: 'error', message: 'A senha deve ter pelo menos 6 caracteres.' });
      return;
    }
    if (password !== confirmPassword) {
      setAuthStatus({ status: 'error', message: 'As senhas informadas não coincidem.' });
      return;
    }

    setAuthStatus({ status: 'loading' });
    const result = await register(email, password, name);
    if (result.ok) {
      if (result.autoVerified) {
        setAuthStatus({ status: 'success', message: 'Conta criada e ativada automaticamente com sucesso! Redirecionando para login...' });
        setTimeout(() => {
          setAuthView('login');
          setAuthStatus({ status: 'idle' });
        }, 3000);
      } else {
        setAuthStatus({ status: 'success', message: result.message || 'Conta criada! Por favor, verifique seu e-mail para ativar.' });
      }
      setEmail('');
      setPassword('');
      setConfirmPassword('');
      setName('');
    } else {
      setAuthStatus({ status: 'error', message: result.message });
    }
  };

  return (
    <div className="min-h-screen bg-[#070b13] text-slate-100 flex items-center justify-center font-sans p-4 relative overflow-hidden">
      <div className="absolute top-1/4 left-1/4 -translate-x-1/2 -translate-y-1/2 w-96 h-96 rounded-full bg-violet-600/10 blur-[100px] pointer-events-none" />
      <div className="absolute bottom-1/4 right-1/4 translate-x-1/2 translate-y-1/2 w-96 h-96 rounded-full bg-cyan-600/10 blur-[100px] pointer-events-none" />

      <div className="glass-card w-full max-w-md border border-white/10 rounded-2xl shadow-2xl p-8 relative z-10 bg-slate-900/60 backdrop-blur-md">
        <div className="flex flex-col items-center gap-2 mb-8 text-center">
          <div className="relative flex items-center justify-center h-16 w-52 overflow-hidden rounded-xl bg-white/5 p-2 border border-white/10 mb-2">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src="https://lirp.cdn-website.com/2cf4cfdc/dms3rep/multi/opt/IT+Facil+-+logo+-+alta-47c0885e-158w.png"
              alt="ITFácil Logo"
              className="h-full w-auto object-contain"
            />
          </div>
          <h1 className="text-xl font-bold uppercase tracking-wider text-white">ITFácil NOC</h1>
          <p className="text-xs text-slate-400">Painel SRE Multi-tenant de Gerenciamento &amp; Auto-cura</p>
        </div>

        {verifiedBanner && (
          <div className="mb-6 p-3 rounded-lg bg-emerald-950/20 border border-emerald-500/20 text-emerald-400 text-xs flex items-center gap-2">
            <CheckCircle2 className="w-4 h-4 shrink-0" />
            <span>E-mail verificado com sucesso! Você já pode realizar o login.</span>
          </div>
        )}

        <div className="flex border-b border-white/5 mb-6">
          <button
            type="button"
            onClick={() => { setAuthView('login'); setAuthStatus({ status: 'idle' }); }}
            className={`flex-1 pb-3 text-sm font-bold transition-all ${authView === 'login' ? 'text-violet-400 border-b-2 border-violet-500' : 'text-slate-400 hover:text-slate-200'}`}
          >
            Acessar Cockpit
          </button>
          <button
            type="button"
            onClick={() => { setAuthView('register'); setAuthStatus({ status: 'idle' }); }}
            className={`flex-1 pb-3 text-sm font-bold transition-all ${authView === 'register' ? 'text-violet-400 border-b-2 border-violet-500' : 'text-slate-400 hover:text-slate-200'}`}
          >
            Criar Conta
          </button>
        </div>

        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          {authView === 'register' && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="name" className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Nome Completo</Label>
              <Input id="name" type="text" required value={name} onChange={(e) => setName(e.target.value)} placeholder="Seu nome" />
            </div>
          )}

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="email" className="text-[10px] uppercase font-bold tracking-wider text-slate-400">E-mail Corporativo</Label>
            <Input id="email" type="email" required value={email} onChange={(e) => handleEmailChange(e.target.value)} placeholder="seu-nome@empresa.com" />
            {authView === 'register' && signupEmailError && (
              <span className="text-[10px] text-rose-400 font-medium px-1 mt-0.5">{signupEmailError}</span>
            )}
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="password" className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Senha</Label>
            <div className="relative flex items-center">
              <Input
                id="password"
                type={authView === 'login' ? (showLoginPassword ? 'text' : 'password') : (showSignupPassword ? 'text' : 'password')}
                required
                value={password}
                onChange={(e) => handlePasswordChange(e.target.value)}
                placeholder={authView === 'login' ? 'Sua senha' : 'Mínimo de 6 caracteres'}
                className="pr-10"
              />
              <button
                type="button"
                onClick={() => (authView === 'login' ? setShowLoginPassword((v) => !v) : setShowSignupPassword((v) => !v))}
                className="absolute right-3 text-slate-400 hover:text-white transition-all cursor-pointer"
              >
                {(authView === 'login' ? showLoginPassword : showSignupPassword) ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
              </button>
            </div>
            {authView === 'register' && signupPasswordError && (
              <span className="text-[10px] text-rose-400 font-medium px-1 mt-0.5">{signupPasswordError}</span>
            )}
          </div>

          {authView === 'register' && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="confirmPassword" className="text-[10px] uppercase font-bold tracking-wider text-slate-400">Confirmar Senha</Label>
              <div className="relative flex items-center">
                <Input
                  id="confirmPassword"
                  type={showSignupConfirmPassword ? 'text' : 'password'}
                  required
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  placeholder="Repita sua senha"
                  className="pr-10"
                />
                <button
                  type="button"
                  onClick={() => setShowSignupConfirmPassword((v) => !v)}
                  className="absolute right-3 text-slate-400 hover:text-white transition-all cursor-pointer"
                >
                  {showSignupConfirmPassword ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                </button>
              </div>
              {confirmPassword && password !== confirmPassword && (
                <span className="text-[10px] text-rose-400 font-medium px-1 mt-0.5">As senhas não coincidem.</span>
              )}
            </div>
          )}

          <Button
            type="submit"
            disabled={authStatus.status === 'loading'}
            className="w-full bg-gradient-to-r from-violet-600 to-indigo-600 hover:from-violet-500 hover:to-indigo-500 text-white font-bold text-xs py-3 mt-2 shadow-md shadow-violet-950/40"
          >
            {authStatus.status === 'loading' && <RefreshCw className="w-4 h-4 animate-spin" />}
            {authView === 'login' ? 'Entrar no Painel' : 'Registrar Minha Conta'}
          </Button>

          {authStatus.status === 'success' && authStatus.message && (
            <div className="p-3 bg-emerald-950/20 border border-emerald-500/20 text-emerald-400 text-xs rounded-lg mt-2 font-sans">
              {authStatus.message}
            </div>
          )}
          {authStatus.status === 'error' && authStatus.message && (
            <div className="p-3 bg-rose-950/20 border border-rose-500/20 text-rose-400 text-xs rounded-lg mt-2 font-sans flex items-center gap-2">
              <AlertTriangle className="w-3.5 h-3.5 shrink-0" />
              <span>{authStatus.message}</span>
            </div>
          )}
        </form>
      </div>
    </div>
  );
}
