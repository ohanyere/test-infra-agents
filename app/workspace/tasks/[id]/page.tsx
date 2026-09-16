import TaskDetail from "../../../scenarios/[id]/task-detail";
export default async function Page({params}:{params:Promise<{id:string}>}){return <TaskDetail taskID={(await params).id}/>}
