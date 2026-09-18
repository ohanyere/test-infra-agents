"use client";

import { useState, type FormEvent, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { apiRequest, APIError } from "../../../lib/api";
import { useAuth } from "../../../lib/auth";
import styles from "./new-scenario.module.css";

type Mode = "description" | "manual" | "incident_import";
type Criterion = { key: string; name: string; metric_key: string; operator: string; target_value?: unknown; unit?: string | null; requires_confirmation?: boolean };
type Draft = { name:string; description:string; category:string; difficulty:string; duration_seconds:number; agent_spec:{task:string;visible_context:unknown;available_observations:string[];allowed_actions:string[];allowed_resources:unknown[]}; environment:unknown; conditions:unknown[]; expected_behaviors:string[]; forbidden_behaviors:string[]; criteria:Criterion[]; ground_truth:unknown; execution_spec:unknown; assumptions:string[]; warnings:string[]; runnable_now:boolean; unsupported_capabilities:string[]; source_metadata?:unknown };
type DraftResponse = { draft:Draft; evidence_summary?:string[]; observed_facts?:string[]; ai_inferences?:string[]; needs_confirmation?:string[] };

const emptyDraft = (): Draft => ({name:"",description:"",category:"incident-response",difficulty:"medium",duration_seconds:600,agent_spec:{task:"",visible_context:{},available_observations:["kubernetes_workloads","kubernetes_pods","kubernetes_events"],allowed_actions:["inspect","scale_workload","restart_pod","no_op"],allowed_resources:[]},environment:{platform:"kubernetes"},conditions:[],expected_behaviors:[],forbidden_behaviors:[],criteria:[],ground_truth:{},execution_spec:{},assumptions:[],warnings:[],runnable_now:false,unsupported_capabilities:[]});
const toLines=(value:string)=>value.split("\n").map(v=>v.trim()).filter(Boolean);
const slugify=(value:string)=>value.toLowerCase().trim().replace(/[^a-z0-9]+/g,"-").replace(/^-|-$/g,"").slice(0,80);

export function ScenarioForm(){
 const [mode,setMode]=useState<Mode|null>(null),[draft,setDraft]=useState<Draft|null>(null),[description,setDescription]=useState(""),[timeline,setTimeline]=useState(""),[files,setFiles]=useState<File[]>([]),[result,setResult]=useState<DraftResponse|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(""); const {workspace}=useAuth();
  if(!mode)return <div className={`${styles.shell} ${styles.form}`}><section className={styles.formSection}><div className={styles.sectionHeading}><h2>How do you want to create this verification scenario?</h2><p>Every path produces the same reviewable, versioned scenario.</p></div><div className={styles.modeList}><ModeButton title="Describe it" copy="Describe the infrastructure situation you want to verify and let Evalplane structure it for you." onClick={()=>setMode("description")}/><ModeButton title="Build manually" copy="Define the environment, conditions, permissions, and verification expectations yourself." onClick={()=>{setMode("manual");setDraft(emptyDraft())}}/><ModeButton title="Import an incident" copy="Turn a real production incident into a replayable verification scenario." onClick={()=>setMode("incident_import")}/></div></section></div>;
 async function generate(event:FormEvent){event.preventDefault();if(!workspace){setError("No workspace is available.");return}setBusy(true);setError("");try{let response:DraftResponse;if(mode==="description")response=await apiRequest(`/v1/workspaces/${workspace.id}/scenario-drafts/describe`,{method:"POST",body:JSON.stringify({description})});else{const body=new FormData();body.set("description",description);body.set("timeline",timeline);files.forEach(file=>body.append("files",file));response=await apiRequest(`/v1/workspaces/${workspace.id}/scenario-drafts/import-incident`,{method:"POST",body})}setResult(response);setDraft(response.draft)}catch(reason){setError(reason instanceof APIError?reason.message:"Scenario generation failed.")}finally{setBusy(false)}}
 if(draft)return <DraftEditor draft={draft} setDraft={setDraft} method={mode} result={result} onBack={()=>{setDraft(null);if(mode==="manual")setMode(null)}}/>;
   return <form className={`${styles.shell} ${styles.form}`} onSubmit={generate}><section className={styles.formSection}><button className={styles.backButton} type="button" onClick={()=>setMode(null)}>← Authoring methods</button><div className={styles.sectionHeading}><h2>{mode==="description"?"Describe what you want to test":"Import a real incident"}</h2><p>{mode==="description"?"Include the environment, symptoms, constraints, desired outcome, and anything the agent must not do.":"Evidence is extracted and redacted before it is sent for analysis."}</p></div><TextArea label={mode==="description"?"Evaluation task description":"Incident context (optional)"} value={description} onChange={setDescription}/>{mode==="incident_import"&&<><TextArea label="Timeline / notes" value={timeline} onChange={setTimeline}/><div className={styles.field}><label htmlFor="files">Evidence</label><input id="files" type="file" multiple accept=".yaml,.yml,.json,.txt,.log,.md" onChange={e=>setFiles(Array.from(e.target.files??[]).slice(0,10))}/><span className={styles.helper}>Up to 10 files; raw uploads are not stored.</span></div>{workspace&&<Connectors workspaceId={workspace.id} onImported={(response)=>{setResult(response);setDraft(response.draft)}}/>}</>}{error&&<p className={styles.error}>{error}</p>}<div className={styles.formActions}><button className={styles.primaryButton} disabled={busy}>{busy?"Working…":mode==="description"?"Generate scenario":"Analyze incident"}</button></div></section></form>
}

function ModeButton({title,copy,onClick}:{title:string;copy:string;onClick:()=>void}){return <button type="button" className={styles.modeRow} onClick={onClick}><strong>{title}</strong><span>{copy}</span><span aria-hidden>→</span></button>}
function Connectors({workspaceId,onImported}:{workspaceId:string;onImported:(response:DraftResponse)=>void}){return <IncidentConnectors workspaceId={workspaceId} onImported={onImported}/>}

const DATADOG_SITES=["DATADOGHQ.COM","US3.DATADOGHQ.COM","US5.DATADOGHQ.COM","DATADOGHQ.EU","AP1.DATADOGHQ.COM","AP2.DATADOGHQ.COM"];
type IncidentConnection={id:string;provider:string;display_name:string;status:string;last_diagnostic_code?:string|null;connector_contract_version?:string};
type IncidentItem={provider:string;external_id:string;title:string;summary?:string;status?:string;severity?:string};
function IncidentConnectors({workspaceId,onImported}:{workspaceId:string;onImported:(response:DraftResponse)=>void}){
 const [provider,setProvider]=useState<"pagerduty"|"datadog">("pagerduty");
 const [connections,setConnections]=useState<IncidentConnection[]>([]);
 const [connectionId,setConnectionId]=useState("");
 const [displayName,setDisplayName]=useState("");
 const [site,setSite]=useState(DATADOG_SITES[0]);
 const [apiToken,setApiToken]=useState("");
 const [apiKey,setApiKey]=useState("");
 const [appKey,setAppKey]=useState("");
 const [query,setQuery]=useState("");
 const [items,setItems]=useState<IncidentItem[]>([]);
 const [selected,setSelected]=useState("");
 const [busy,setBusy]=useState(false);
 const [message,setMessage]=useState("");
 async function refresh(){
  setBusy(true);setMessage("");
  try{
   const data=await apiRequest<{items:IncidentConnection[]}>(`/v1/workspaces/${workspaceId}/incident-sources`,{});
   const filtered=data.items.filter(c=>c.provider===provider);
   setConnections(filtered);
   if(!filtered.some(c=>c.id===connectionId))setConnectionId(filtered[0]?.id??"");
  }catch(reason){setMessage(reason instanceof APIError?reason.message:"Could not list incident connections.")}
  finally{setBusy(false)}
 }
 async function createConnection(){
  setBusy(true);setMessage("");
  try{
   const body:Record<string,string>={provider,display_name:displayName};
   if(provider==="pagerduty")body.api_token=apiToken;
   else{body.site=site;body.api_key=apiKey;body.application_key=appKey}
   const created=await apiRequest<IncidentConnection>(`/v1/workspaces/${workspaceId}/incident-sources`,{method:"POST",body:JSON.stringify(body)});
   setApiToken("");setApiKey("");setAppKey("");
   await refresh();setConnectionId(created.id);
  }catch(reason){setMessage(reason instanceof APIError?reason.message:"Connection could not be created.")}
  finally{setBusy(false)}
 }
 async function testConnection(){
  if(!connectionId)return;
  setBusy(true);setMessage("");
  try{
   const updated=await apiRequest<IncidentConnection>(`/v1/workspaces/${workspaceId}/incident-sources/${connectionId}/test`,{method:"POST"});
   setConnections(list=>list.map(c=>c.id===updated.id?updated:c));
   setMessage(`Status: ${updated.status}${updated.last_diagnostic_code?` (${updated.last_diagnostic_code})`:""}`);
  }catch(reason){setMessage(reason instanceof APIError?reason.message:"Connection test failed.")}
  finally{setBusy(false)}
 }
 async function browse(){
  if(!connectionId)return;
  setBusy(true);setMessage("");
  try{
   const params=new URLSearchParams({limit:"20",offset:"0"});
   if(query.trim())params.set("query",query.trim());
   const page=await apiRequest<{items:IncidentItem[]}>(`/v1/workspaces/${workspaceId}/incident-sources/${connectionId}/incidents?${params.toString()}`,{});
   setItems(page.items??[]);
   if(!page.items?.some(i=>i.external_id===selected))setSelected(page.items?.[0]?.external_id??"");
  }catch(reason){setMessage(reason instanceof APIError?reason.message:"Browse failed.")}
  finally{setBusy(false)}
 }
 async function importSelected(){
  if(!connectionId||!selected)return;
  setBusy(true);setMessage("");
  try{
   const result=await apiRequest<{incident:IncidentItem;draft_result?:DraftResponse;draft?:DraftResponse}>(`/v1/workspaces/${workspaceId}/incident-sources/${connectionId}/incidents/${encodeURIComponent(selected)}/import`,{method:"POST"});
   const draftResponse=result.draft_result??result.draft;
   if(!draftResponse?.draft){setMessage("Import succeeded but returned no draft.");return}
   onImported(draftResponse);
  }catch(reason){setMessage(reason instanceof APIError?reason.message:"Import failed.")}
  finally{setBusy(false)}
 }
 return <section className={styles.connectors}>
  <h3>Connect an incident source (read-only)</h3>
  <p><small>Token / API-key auth only — no OAuth. No continuous sync, no webhooks, no provider writes. Evidence is redacted before analysis.</small></p>
  <div className={styles.field}><label>Provider</label><select value={provider} onChange={e=>{setProvider(e.target.value as "pagerduty"|"datadog");setItems([]);setSelected("")}}><option value="pagerduty">PagerDuty (read-only)</option><option value="datadog">Datadog (read-only)</option></select></div>
  <div className={styles.formActions}><button type="button" className={styles.backButton} onClick={refresh} disabled={busy}>Refresh connections</button></div>
  {connections.length>0&&<div className={styles.field}><label>Connection</label><select value={connectionId} onChange={e=>setConnectionId(e.target.value)}>{connections.map(c=><option key={c.id} value={c.id}>{c.display_name} — {c.status}</option>)}</select></div>}
  <div className={styles.field}><label>New connection display name</label><input value={displayName} onChange={e=>setDisplayName(e.target.value)} placeholder="prod-pagerduty-readonly"/></div>
  {provider==="pagerduty"
   ?<div className={styles.field}><label>PagerDuty API token</label><input type="password" value={apiToken} onChange={e=>setApiToken(e.target.value)} placeholder="v2 API token" autoComplete="off"/></div>
   :<><div className={styles.field}><label>Datadog site</label><select value={site} onChange={e=>setSite(e.target.value)}>{DATADOG_SITES.map(s=><option key={s} value={s}>{s}</option>)}</select></div>
   <div className={styles.field}><label>Datadog API key</label><input type="password" value={apiKey} onChange={e=>setApiKey(e.target.value)} autoComplete="off"/></div>
   <div className={styles.field}><label>Datadog application key</label><input type="password" value={appKey} onChange={e=>setAppKey(e.target.value)} autoComplete="off"/></div></>}
  <div className={styles.formActions}><button type="button" className={styles.backButton} onClick={createConnection} disabled={busy||!displayName.trim()}>Save connection</button>{connectionId&&<button type="button" className={styles.backButton} onClick={testConnection} disabled={busy}>Test connection</button>}</div>
  {connectionId&&<><div className={styles.field}><label>Browse incidents</label><input value={query} onChange={e=>setQuery(e.target.value)} placeholder="optional search (max 200 chars)"/></div>
  <div className={styles.formActions}><button type="button" className={styles.backButton} onClick={browse} disabled={busy}>Browse (max 20)</button></div>
  {items.length>0&&<div className={styles.field}><label>Select incident</label><select value={selected} onChange={e=>setSelected(e.target.value)}>{items.map(i=><option key={i.external_id} value={i.external_id}>{i.title||i.external_id} ({i.external_id})</option>)}</select></div>}
  {selected&&<div className={styles.formActions}><button type="button" className={styles.primaryButton} onClick={importSelected} disabled={busy}>Import into draft</button></div>}</>}
  {message&&<p className={styles.helper}>{message}</p>}
  {busy&&<p className={styles.helper}>Working…</p>}
 </section>
}

function DraftEditor({draft,setDraft,method,result,onBack}:{draft:Draft;setDraft:(d:Draft)=>void;method:Mode;result:DraftResponse|null;onBack:()=>void}){
 const {workspace}=useAuth();const router=useRouter();const [busy,setBusy]=useState(false),[message,setMessage]=useState("");
 const setJSON=(key:"environment"|"conditions"|"ground_truth"|"execution_spec",value:string)=>{try{setDraft({...draft,[key]:JSON.parse(value)});setMessage("")}catch{setMessage(`${key.replaceAll("_"," ")} must be valid JSON.`)}};
 async function save(event:FormEvent){event.preventDefault();if(!workspace){setMessage("No workspace is available.");return}setBusy(true);setMessage("");try{const criteria=draft.criteria.filter(c=>!c.requires_confirmation&&c.target_value!==undefined).map((c,sort_order)=>({...c,sort_order}));await apiRequest(`/v1/workspaces/${workspace.id}/scenarios`,{method:"POST",body:JSON.stringify({name:draft.name,slug:slugify(draft.name),visibility:"workspace",initial_version:{description:draft.description,category:slugify(draft.category),difficulty:draft.difficulty,duration_seconds:draft.duration_seconds,environment:draft.environment,conditions:draft.conditions,observables:draft.agent_spec.available_observations,expected_behaviors:draft.expected_behaviors,forbidden_behaviors:draft.forbidden_behaviors,criteria,authoring_method:method,agent_task:draft.agent_spec.task,agent_visible_context:draft.agent_spec.visible_context,allowed_actions:draft.agent_spec.allowed_actions,allowed_resources:draft.agent_spec.allowed_resources,ground_truth:draft.ground_truth,execution_spec:draft.execution_spec,source_metadata:draft.source_metadata??{authoring_method:method}}})});router.push("/scenarios");router.refresh()}catch(reason){setMessage(reason instanceof APIError?reason.message:"Evaluation task could not be saved.")}finally{setBusy(false)}}
 return <form className={`${styles.shell} ${styles.form}`} onSubmit={save}><section className={styles.formSection}><button className={styles.backButton} type="button" onClick={onBack}>← Back</button><div className={styles.sectionHeading}><h2>Review evaluation task</h2><p>Edit the canonical task before saving it.</p></div>{result?.evidence_summary?.length?<Notice title="Imported evidence summary" items={result.evidence_summary}/>:null}{result?.observed_facts?.length?<Notice title="Observed facts" items={result.observed_facts}/>:null}{result?.ai_inferences?.length?<Notice title="AI inferences" items={result.ai_inferences}/>:null}{result?.needs_confirmation?.length?<Notice title="Needs confirmation" items={result.needs_confirmation}/>:null}<TextField label="Evaluation task name" value={draft.name} onChange={name=>setDraft({...draft,name})}/><TextArea label="Description" value={draft.description} onChange={description=>setDraft({...draft,description})}/><div className={styles.threeFields}><TextField label="Category" value={draft.category} onChange={category=>setDraft({...draft,category})}/><TextField label="Difficulty" value={draft.difficulty} onChange={difficulty=>setDraft({...draft,difficulty})}/><TextField label="Duration seconds" value={String(draft.duration_seconds)} onChange={v=>setDraft({...draft,duration_seconds:Number(v)||0})}/></div></section>
 <Section title="Environment"><JSONArea value={draft.environment} onChange={v=>setJSON("environment",v)}/></Section><Section title="Agent task"><TextArea label="Task shown to the agent" value={draft.agent_spec.task} onChange={task=>setDraft({...draft,agent_spec:{...draft.agent_spec,task}})}/></Section><Section title="Agent-visible context"><JSONArea value={draft.agent_spec.visible_context} onChange={v=>{try{setDraft({...draft,agent_spec:{...draft.agent_spec,visible_context:JSON.parse(v)}})}catch{setMessage("Agent-visible context must be valid JSON.")}}}/></Section><Section title="Allowed observations"><Lines value={draft.agent_spec.available_observations} onChange={available_observations=>setDraft({...draft,agent_spec:{...draft.agent_spec,available_observations}})}/></Section><Section title="Allowed actions"><Lines value={draft.agent_spec.allowed_actions} onChange={allowed_actions=>setDraft({...draft,agent_spec:{...draft.agent_spec,allowed_actions}})}/></Section><Section title="Allowed resources"><JSONArea value={draft.agent_spec.allowed_resources} onChange={v=>{try{setDraft({...draft,agent_spec:{...draft.agent_spec,allowed_resources:JSON.parse(v)}})}catch{setMessage("Allowed resources must be valid JSON.")}}}/></Section><Section title="Conditions"><JSONArea value={draft.conditions} onChange={v=>setJSON("conditions",v)}/></Section><Section title="Expected behavior"><Lines value={draft.expected_behaviors} onChange={expected_behaviors=>setDraft({...draft,expected_behaviors})}/></Section><Section title="Forbidden behavior"><Lines value={draft.forbidden_behaviors} onChange={forbidden_behaviors=>setDraft({...draft,forbidden_behaviors})}/></Section><Section title="Qualification criteria"><JSONArea value={draft.criteria} onChange={v=>{try{setDraft({...draft,criteria:JSON.parse(v)})}catch{setMessage("Criteria must be valid JSON.")}}}/></Section>
 <section className={`${styles.formSection} ${styles.evaluatorOnly}`}><div className={styles.sectionHeading}><h2>Evaluator only — not shown to the agent</h2><p>Ground truth and execution details remain outside the agent projection.</p></div><label>Ground truth</label><JSONArea value={draft.ground_truth} onChange={v=>setJSON("ground_truth",v)}/><label>Execution specification</label><JSONArea value={draft.execution_spec} onChange={v=>setJSON("execution_spec",v)}/></section>{draft.assumptions.length?<Notice title="Assumptions" items={draft.assumptions}/>:null}{draft.warnings.length?<Notice title="Warnings" items={draft.warnings}/>:null}{draft.unsupported_capabilities.length?<Notice title="Unsupported capabilities" items={draft.unsupported_capabilities}/>:null}{message&&<p className={styles.error}>{message}</p>}<div className={styles.formActions}><button className={styles.primaryButton} disabled={busy}>{busy?"Saving…":"Save scenario"}</button><button className={styles.backButton} type="button" onClick={onBack}>Back</button></div></form>
}
function Section({title,children}:{title:string;children:ReactNode}){return <section className={styles.formSection}><div className={styles.sectionHeading}><h2>{title}</h2></div>{children}</section>}
function TextField({label,value,onChange}:{label:string;value:string;onChange:(v:string)=>void}){return <div className={styles.field}><label>{label}</label><input value={value} onChange={e=>onChange(e.target.value)} required/></div>}
function TextArea({label,value,onChange}:{label:string;value:string;onChange:(v:string)=>void}){return <div className={styles.field}><label>{label}</label><textarea rows={6} value={value} onChange={e=>onChange(e.target.value)}/></div>}
function JSONArea({value,onChange}:{value:unknown;onChange:(v:string)=>void}){return <textarea className={styles.jsonArea} rows={8} defaultValue={JSON.stringify(value,null,2)} onBlur={e=>onChange(e.target.value)}/>}
function Lines({value,onChange}:{value:string[];onChange:(v:string[])=>void}){return <textarea className={styles.jsonArea} rows={5} value={value.join("\n")} onChange={e=>onChange(toLines(e.target.value))}/>}
function Notice({title,items}:{title:string;items:string[]}){return <aside className={styles.notice}><h3>{title}</h3><ul>{items.map((item,index)=><li key={`${item}-${index}`}>{item}</li>)}</ul></aside>}
