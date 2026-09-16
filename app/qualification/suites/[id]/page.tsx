import SuiteDetail from "./suite-detail";
export default async function Page({params}:{params:Promise<{id:string}>}){return <SuiteDetail suiteID={(await params).id}/>}
