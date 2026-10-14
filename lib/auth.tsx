"use client";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { usePathname, useRouter } from "next/navigation";
import { APIError, apiRequest, type AuthMeResponse, type CurrentWorkspace } from "./api";

type AuthState = { data: AuthMeResponse | null; workspace: CurrentWorkspace | null; loading: boolean; error: string; retry: () => void; logout: () => Promise<void> };
const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const router = useRouter(); const pathname=usePathname(); const [data,setData]=useState<AuthMeResponse|null>(null); const [loading,setLoading]=useState(true); const [error,setError]=useState(""); const [attempt,setAttempt]=useState(0);
  const requiresAuth=pathname==="/onboarding"||pathname==="/workspace"||pathname.startsWith("/workspace/")||pathname==="/scenarios/new"||pathname==="/agents"||pathname.startsWith("/agents/")||pathname==="/environments"||pathname.startsWith("/environments/")||pathname==="/qualification"||pathname.startsWith("/qualification/")||pathname==="/runs"||pathname.startsWith("/runs/")||pathname==="/verifications"||pathname.startsWith("/verifications/")||pathname.startsWith("/evaluations/")||pathname.startsWith("/comparisons/")||pathname.startsWith("/suite-executions/");
  useEffect(()=>{let active=true;let redirecting=false;setLoading(true);setError("");apiRequest<AuthMeResponse>("/v1/auth/me").then((value)=>{if(active)setData(value)}).catch((reason:unknown)=>{if(!active)return;if(reason instanceof APIError&&reason.status===401){setData(null);if(requiresAuth){redirecting=true;router.replace("/login")}return}setError(reason instanceof APIError?reason.message:"We couldn't reach the service. Try again.")}).finally(()=>{if(active&&!redirecting)setLoading(false)});return()=>{active=false}},[attempt,requiresAuth,router]);
  const retry=useCallback(()=>setAttempt((value)=>value+1),[]);
  const logout=useCallback(async()=>{try{await apiRequest<void>("/v1/auth/logout",{method:"POST"})}catch(reason){if(!(reason instanceof APIError)||reason.status!==401)throw reason}finally{setData(null);router.replace("/login");router.refresh()}},[router]);
  const value=useMemo(()=>({data,workspace:data?.workspaces[0]??null,loading,error,retry,logout}),[data,loading,error,retry,logout]);
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
export function useAuth(){const value=useContext(AuthContext);if(!value)throw new Error("useAuth must be used inside AuthProvider");return value}
