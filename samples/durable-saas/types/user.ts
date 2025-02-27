export interface User {
    id: string;
    firstname: string;
    lastname: string;
    email: string;
    password: string;
    confirmed: boolean;
}

export interface WriteUserRequest {
    firstname: string;
    lastname: string;
    email: string;
    password: string;
    confirmed: boolean;
}

export interface SignupRequest {
    firstname: string;
    lastname: string;
    email: string;
    password: string;
}

export interface SignupResponse {
    workflow_id: string;
    user: User;
}